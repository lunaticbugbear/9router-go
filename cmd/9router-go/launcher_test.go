package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// listenPort extracts the port from an httptest server URL.
func listenPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	_, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split %q: %v", u.Host, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port %q: %v", portStr, err)
	}
	return port
}

// osPipe returns a connected pipe pair, used to prove a non-char-device stdin
// is not treated as a TTY.
func osPipe() (*os.File, *os.File, error) {
	return os.Pipe()
}

// createTempFile returns an open regular file, the other non-TTY case.
func createTempFile(t *testing.T) (*os.File, error) {
	t.Helper()
	return os.CreateTemp(t.TempDir(), "stdin")
}

// getenv reads an env value without shadowing the stdlib name.
func getenv(t *testing.T, key string) string {
	t.Helper()
	return os.Getenv(key)
}

func TestParseMenuChoice(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want menuChoice
	}{
		{"one", "1", menuChoiceOpenWeb},
		{"two", "2", menuChoiceLogs},
		{"three", "3", menuChoiceExit},
		{"newline trimmed", "2\n", menuChoiceLogs},
		{"surrounding spaces", "  1  \n", menuChoiceOpenWeb},
		{"blank line is not a choice", "\n", menuChoiceUnknown},
		{"empty", "", menuChoiceUnknown},
		{"unknown digit", "9\n", menuChoiceUnknown},
		{"out of range", "0\n", menuChoiceUnknown},
		{"words", "web\n", menuChoiceUnknown},
		{"multi-digit not truncated", "13\n", menuChoiceUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseMenuChoice(tc.in); got != tc.want {
				t.Errorf("parseMenuChoice(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLauncherURLs(t *testing.T) {
	if got, want := healthURL(20130), "http://127.0.0.1:20130/health"; got != want {
		t.Errorf("healthURL = %q, want %q", got, want)
	}
	if got, want := serverURL(20261), "http://localhost:20261"; got != want {
		t.Errorf("serverURL = %q, want %q", got, want)
	}
	if got, want := dashboardURL(20261), "http://localhost:20261/dashboard"; got != want {
		t.Errorf("dashboardURL = %q, want %q", got, want)
	}
}

func TestBanner(t *testing.T) {
	got := banner("1.9.0", 20130)
	want := "\n🚀 9router-go v1.9.0\nServer: http://localhost:20130\nDashboard: http://localhost:20130/dashboard\n"
	if got != want {
		t.Errorf("banner mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestMenuText(t *testing.T) {
	want := "\n  1) Web UI (Open in Browser)\n  2) Terminal/Go server logs\n  3) Exit\n"
	if got := menuText(); got != want {
		t.Errorf("menuText mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestWaitForHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	port := listenPort(t, srv.URL)

	if !waitForHealthy(context.Background(), port, 2*time.Second) {
		t.Fatal("expected a healthy server to be detected")
	}
	// A closed port must time out rather than hang or claim readiness.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedPort := listenPort(t, closed.URL)
	closed.Close()
	start := time.Now()
	if waitForHealthy(context.Background(), closedPort, 300*time.Millisecond) {
		t.Fatal("expected no readiness from a closed port")
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("returned before the timeout elapsed (%v)", elapsed)
	}

	// A cancelled context stops the poll promptly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForHealthy(ctx, closedPort, 5*time.Second) {
		t.Fatal("expected cancelled context to report no readiness")
	}
}

func TestWaitForHealthy_Non200IsNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if waitForHealthy(context.Background(), listenPort(t, srv.URL), 250*time.Millisecond) {
		t.Fatal("expected 500 to be treated as not ready")
	}
}

func TestIsTTY(t *testing.T) {
	// A pipe is never a terminal.
	r, w, err := osPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if isTTY(r) {
		t.Error("pipe must not be reported as a TTY")
	}

	f, err := createTempFile(t)
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Error("regular file must not be reported as a TTY")
	}

	// /dev/null is a character device but not interactive. A backgrounded
	// process gets it as stdin, and showing it an interactive menu would both
	// confuse the exit path and hide signal-driven shutdown.
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	if isTTY(devNull) {
		t.Errorf("%s must not be reported as a TTY", os.DevNull)
	}
}

func TestRunTTYLauncher_NeverOpensBrowserOnLaunch(t *testing.T) {
	// The launcher must not launch a browser by itself. `open` is replaced with
	// a recorder: if the banner path ever shelled out, the call would be
	// visible here as a log line.
	recorder := withFakeBrowserOpener(t)
	var out strings.Builder
	opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader("3\n")}
	if err := runTTYLauncher(opts, "1.9.0"); err != nil {
		t.Fatalf("runTTYLauncher: %v", err)
	}
	if calls := recorder.Read(); calls != "" {
		t.Errorf("launch opened a browser unprompted: %s", calls)
	}
	if !strings.HasPrefix(out.String(), banner("1.9.0", 20130)) {
		t.Errorf("banner missing; got:\n%s", out.String())
	}
}

func TestRunInteractiveMenu_OnlyChoiceOneOpensBrowser(t *testing.T) {
	// Option 1 is the sole path that launches a browser. Options 2/3 (and
	// invalid input) must not.
	tests := []struct {
		name     string
		input    string
		wantOpen bool
	}{
		{"choice 1 opens", "1\n3\n", true},
		{"choice 2 does not", "2\n3\n", false},
		{"choice 3 does not", "3\n", false},
		{"invalid input does not", "9\n3\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := withFakeBrowserOpener(t)
			var out strings.Builder
			opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader(tc.input)}
			if err := runInteractiveMenu(opts); err != nil {
				t.Fatalf("menu returned error: %v", err)
			}
			got := recorder.Read()
			if tc.wantOpen && got == "" {
				t.Error("choice 1 did not attempt to open the dashboard")
			}
			if !tc.wantOpen && got != "" {
				t.Errorf("unexpected browser launch: %s", got)
			}
		})
	}
}

// withFakeBrowserOpener makes openDashboard a no-op that records its URL, so
// tests can prove exactly when a browser would have been launched. It returns
// the recorder; the real implementation is restored automatically.
func withFakeBrowserOpener(t *testing.T) *browserCallRecorder {
	t.Helper()
	rec := &browserCallRecorder{}
	prev := browserLauncher
	browserLauncher = rec.launch
	t.Cleanup(func() { browserLauncher = prev })
	return rec
}

// browserCallRecorder collects the URLs a (stubbed) browser launch received.
type browserCallRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *browserCallRecorder) launch(url string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, url)
	return nil
}

func (r *browserCallRecorder) Read() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.calls, "\n")
}

func TestApplyLauncherEnv(t *testing.T) {
	t.Setenv("PORT", "11111")
	t.Setenv("HOST", "127.0.0.1")

	applyLauncherEnv(20261, "0.0.0.0")
	if got := getenv(t, "PORT"); got != "20261" {
		t.Errorf("PORT = %q, want 20261", got)
	}
	if got := getenv(t, "HOST"); got != "0.0.0.0" {
		t.Errorf("HOST = %q, want 0.0.0.0", got)
	}

	// Zero/empty flags leave the environment-sourced values untouched.
	applyLauncherEnv(0, "")
	if got := getenv(t, "PORT"); got != "20261" {
		t.Errorf("PORT changed by zero flag: %q", got)
	}
	if got := getenv(t, "HOST"); got != "0.0.0.0" {
		t.Errorf("HOST changed by empty flag: %q", got)
	}
}

// runMenu feeds input to the interactive loop and returns its output and error.
func runMenu(t *testing.T, in string) (string, error) {
	t.Helper()
	var out strings.Builder
	opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader(in)}
	err := runInteractiveMenu(opts)
	return out.String(), err
}

func TestRunInteractiveMenu_Exit(t *testing.T) {
	out, err := runMenu(t, "3\n")
	if err != nil {
		t.Fatalf("exit choice returned error: %v", err)
	}
	for _, want := range []string{"Select [1-3]: ", "Shutting down the gateway..."} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunInteractiveMenu_LogsThenExit(t *testing.T) {
	out, err := runMenu(t, "2\n3\n")
	if err != nil {
		t.Fatalf("menu returned error: %v", err)
	}
	for _, want := range []string{
		"The gateway logs stream in this terminal, above this menu.",
		"Server: http://localhost:20130",
		"Dashboard: http://localhost:20130/dashboard",
		"Health: http://127.0.0.1:20130/health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
	// The menu is redrawn after handling a choice, so the prompt appears twice.
	if n := strings.Count(out, "Select [1-3]: "); n != 2 {
		t.Errorf("expected menu redrawn once (2 prompts), got %d", n)
	}
}

func TestRunInteractiveMenu_RejectsUnknownInput(t *testing.T) {
	out, err := runMenu(t, "9\n3\n")
	if err != nil {
		t.Fatalf("menu returned error: %v", err)
	}
	if !strings.Contains(out, "Please choose 1, 2 or 3.") {
		t.Errorf("expected rejection message; got:\n%s", out)
	}
}

func TestRunInteractiveMenu_EOF(t *testing.T) {
	out, err := runMenu(t, "")
	if err == nil {
		t.Fatal("expected an error on EOF so the caller can fall back to babysitting")
	}
	if !strings.Contains(out, menuText()) {
		t.Errorf("menu was not drawn before EOF; got:\n%s", out)
	}
}

func TestRunInteractiveMenu_WebChoiceIsHandled(t *testing.T) {
	// Choice 1 must return to the menu rather than exiting or looping forever.
	// Whether a browser launches is covered by the recorder test above; here we
	// pin the loop's control flow with a stub that cannot touch the desktop.
	withFakeBrowserOpener(t)
	var out strings.Builder
	opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader("1\n3\n")}
	if err := runInteractiveMenu(opts); err != nil {
		t.Fatalf("menu returned error: %v", err)
	}
	if n := strings.Count(out.String(), "Select [1-3]: "); n != 2 {
		t.Errorf("choice 1 did not return to the menu (prompts=%d); got:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "Shutting down the gateway...") {
		t.Errorf("choice 3 after choice 1 did not shut down; got:\n%s", out.String())
	}
}

func TestOpenDashboard_PrintsURLWhenLauncherUnavailable(t *testing.T) {
	// When no browser can be launched, the URL must still reach the user.
	withFakeBrowserOpener(t)
	browserLauncher = func(string) error { return errNoBrowserAvailable }

	var out strings.Builder
	openDashboard("http://localhost:1/dashboard", &out)
	want := "Open the dashboard manually: http://localhost:1/dashboard"
	if !strings.Contains(out.String(), want) {
		t.Errorf("expected %q; got:\n%s", want, out.String())
	}
}

func TestOpenDashboard_SilentOnSuccess(t *testing.T) {
	// A successful launch prints nothing (the browser is the feedback).
	rec := withFakeBrowserOpener(t)

	var out strings.Builder
	openDashboard("http://localhost:1/dashboard", &out)
	if out.String() != "" {
		t.Errorf("expected no fallback output on success; got:\n%s", out.String())
	}
	if got := rec.Read(); got != "http://localhost:1/dashboard" {
		t.Errorf("launcher received %q, want the dashboard URL", got)
	}
}

func TestRunTTYLauncher_PrintsBannerAndMenu(t *testing.T) {
	var out strings.Builder
	opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader("3\n")}
	if err := runTTYLauncher(opts, "1.9.0"); err != nil {
		t.Fatalf("runTTYLauncher: %v", err)
	}
	got := out.String()
	if !strings.HasPrefix(got, banner("1.9.0", 20130)) {
		t.Errorf("output does not start with the banner; got:\n%s", got)
	}
	if !strings.Contains(got, menuText()) {
		t.Errorf("menu missing; got:\n%s", got)
	}
}

func TestShutdownTrigger_FirstSignalStopsTheProcess(t *testing.T) {
	// Regression guard: the force-quit watchdog must not consume the first
	// signal, or the main wait would block forever waiting for a second one
	// (a lone TERM then failed to stop the gateway).
	trig := newShutdownTrigger()
	forced := make(chan struct{}, 1)
	trig.Arm(func() { forced <- struct{}{} })

	trig.signals <- syscall.SIGTERM

	select {
	case <-trig.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first signal was swallowed; shutdown would never begin")
	}
	select {
	case <-forced:
		t.Fatal("first signal escalated to force-quit instead of graceful shutdown")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestShutdownTrigger_SecondSignalForceQuits(t *testing.T) {
	trig := newShutdownTrigger()
	forced := make(chan struct{}, 1)
	trig.Arm(func() { forced <- struct{}{} })

	trig.signals <- syscall.SIGTERM
	<-trig.Done()
	trig.signals <- syscall.SIGTERM

	select {
	case <-forced:
	case <-time.After(2 * time.Second):
		t.Fatal("second signal did not force quit")
	}
}

func TestShutdownTrigger_RequestIsIdempotent(t *testing.T) {
	trig := newShutdownTrigger()
	trig.Request()
	trig.Request() // must not panic on double close
	select {
	case <-trig.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed after Request")
	}
}

func TestShutdownTrigger_ArmIsIdempotent(t *testing.T) {
	trig := newShutdownTrigger()
	trig.Arm(nil)
	trig.Arm(nil) // a second arm must not spawn a second watchdog
	trig.signals <- syscall.SIGTERM
	select {
	case <-trig.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("signal not observed after double Arm")
	}
}

func TestRunInteractiveMenu_StopRequestInterruptsPrompt(t *testing.T) {
	// The menu blocks on stdin. A stop request arriving at the prompt must end
	// the loop gracefully rather than being ignored until the next keystroke.
	out := &syncBuffer{}
	trig := newShutdownTrigger()
	blocking := &blockingReader{release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- runInteractiveMenu(launcherOptions{Port: 20130, Out: out, In: blocking, Shutdown: trig})
	}()
	defer close(blocking.release)

	waitForOutput(t, out, "Select [1-3]: ")
	trig.Request()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stop request should end the menu cleanly, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("menu ignored the stop request and stayed blocked on stdin")
	}
	if !strings.Contains(out.String(), "Shutting down the gateway...") {
		t.Errorf("no shutdown message after stop request; got:\n%s", out.String())
	}
}

func TestRunInteractiveMenu_ExitRequestsShutdown(t *testing.T) {
	// Choice 3 must mark the process for shutdown, not merely stop reading:
	// the caller relies on Done() to know a graceful stop is wanted.
	trig := newShutdownTrigger()
	var out syncBuffer
	opts := launcherOptions{Port: 20130, Out: &out, In: strings.NewReader("3\n"), Shutdown: trig}
	if err := runInteractiveMenu(opts); err != nil {
		t.Fatalf("menu returned error: %v", err)
	}
	select {
	case <-trig.Done():
	case <-time.After(time.Second):
		t.Fatal("choice 3 did not request shutdown")
	}
}

// syncBuffer is a concurrency-safe string sink: the menu writes from its own
// goroutine while the test polls for its output.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// blockingReader blocks in Read until release is closed, simulating a terminal
// waiting for a keystroke that never comes.
type blockingReader struct {
	release chan struct{}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

// waitForOutput polls until out contains want or the deadline elapses.
func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; got:\n%s", want, out.String())
}
