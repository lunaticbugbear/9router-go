package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// launcherOptions carries the resolved launcher settings for a bare `9router-go` run.
type launcherOptions struct {
	Port int
	Out  io.Writer
	In   io.Reader
	// Shutdown observes stop requests. A nil value is treated as "never stops
	// on its own", which keeps the menu usable from tests that only exercise
	// input dispatch.
	Shutdown *shutdownTrigger
	// Personas is the persona store the loader menu uses. It is the shared
	// settings repository, so an edit made here is visible to the gateway on its
	// next request without a restart. A nil value disables the submenu.
	Personas PersonaStore
}

// shutdownOrIdle returns the configured trigger or a fresh one that is never
// fired, so callers never have to nil-check.
func (o launcherOptions) shutdownOrIdle() *shutdownTrigger {
	if o.Shutdown == nil {
		return newShutdownTrigger()
	}
	return o.Shutdown
}

// The two URLs differ on purpose: readiness must probe the loopback the server
// actually binds, while the printed/browser URLs stay "localhost" for display.
func healthURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/health", port)
}

func serverURL(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

func dashboardURL(port int) string {
	return serverURL(port) + "/dashboard"
}

// waitForHealthy polls /health until it answers 200 or the timeout elapses.
// A short interval keeps startup snappy; the bound keeps a wedged server from
// hanging the launcher forever.
func waitForHealthy(ctx context.Context, port int, timeout time.Duration) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL(port), nil)
		if err != nil {
			return false
		}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// banner is the exact block the shell wrapper printed: leading blank line, then
// the rocket line, server URL, and dashboard URL.
func banner(version string, port int) string {
	return fmt.Sprintf("\n🚀 9router-go v%s\nServer: %s\nDashboard: %s\n",
		version, serverURL(port), dashboardURL(port))
}

// menuText is the TTY menu, matching the validated launcher wrapper wording.
func menuText() string {
	return "\n  1) Web UI (Open in Browser)\n  2) Terminal/Go server logs\n  3) Persona loader\n  4) Exit\n"
}

// menuChoice is the dispatch of one menu line. It is deliberately free of I/O so
// the loop's behavior is table-testable.
type menuChoice int

// errMenuStdinClosed reports that the interactive menu lost its input stream
// (EOF or a read failure); the caller then falls back to signal babysitting.
var errMenuStdinClosed = errors.New("menu: stdin closed")

const (
	menuChoiceUnknown menuChoice = iota
	menuChoiceOpenWeb
	menuChoiceLogs
	menuChoicePersona
	menuChoiceExit
)

// parseMenuChoice maps raw stdin input to a menu action. Any unrecognized
// non-empty input (or a blank line) reports a wrong-selection request; only the
// menu-driven EOF is handled by the caller and never reaches this function.
func parseMenuChoice(raw string) menuChoice {
	switch strings.TrimSpace(raw) {
	case "1":
		return menuChoiceOpenWeb
	case "2":
		return menuChoiceLogs
	case "3":
		return menuChoicePersona
	case "4":
		return menuChoiceExit
	default:
		return menuChoiceUnknown
	}
}

// isTTY reports whether f is an interactive terminal. Character-device alone is
// not enough: /dev/null is a char device but is not interactive, and treating it
// as a TTY would show an interactive menu to a process with no user attached.
// On Unix the reliable test is whether the descriptor has a controlling
// terminal (isatty); on Windows the console-mode check stands in for it.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return hasControllingTerminal(f)
}

// browserLauncher is the platform browser-launch hook. It is a variable so
// tests can substitute a recorder and prove exactly when a launch happens.
var browserLauncher = func(url string) error {
	if runtime.GOOS != "darwin" {
		return errNoBrowserAvailable
	}
	return exec.Command("open", url).Run()
}

// errNoBrowserAvailable reports that this platform has no browser launcher wired.
var errNoBrowserAvailable = errors.New("no browser launcher for this platform")

// openDashboard opens url in the platform browser, best-effort. It runs ONLY
// from the interactive menu's "Web UI" choice — the launcher never opens a
// browser on its own. If no launcher is available (or it fails) the URL is
// printed so the user still has a way in.
func openDashboard(url string, out io.Writer) {
	if err := browserLauncher(url); err == nil {
		return
	}
	fmt.Fprintf(out, "Open the dashboard manually: %s\n", url)
}

// menuSource is the single stdin reader shared by the top-level menu and the
// persona submenu.
//
// Stdin is read on a helper goroutine so a SIGTERM arriving while the loop sits
// at the prompt interrupts the menu instead of being ignored until the next
// keystroke. The goroutine owns the buffered reader, so NOTHING else may read
// stdin directly: a second direct reader would race it and silently lose
// whichever keystrokes the goroutine consumed first. Both menus therefore pull
// lines from this source, and a line that arrives while no prompt is active is
// held as `pending` rather than dropped.
type menuSource struct {
	lines    chan string
	readErr  chan error
	shutdown *shutdownTrigger
	// pending holds a line consumed from the channel while no prompt was
	// active — e.g. the operator typed ahead during startup or before a prompt
	// was drawn. It is drained before the channel so nothing is lost.
	pending string
}

// newMenuSource starts the reader goroutine over in.
func newMenuSource(in io.Reader, shutdown *shutdownTrigger) *menuSource {
	src := &menuSource{
		lines:    make(chan string),
		readErr:  make(chan error, 1),
		shutdown: shutdown,
	}
	reader := bufio.NewReader(in)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				src.readErr <- err
				return
			}
			src.lines <- line
		}
	}()
	return src
}

// readLine returns the next line. It reports stop=true when a shutdown request
// arrived (the caller then unwinds its loop), and errMenuStdinClosed when stdin
// ended. A pending line is always consumed before the channel.
func (s *menuSource) readLine() (line string, stop bool, err error) {
	if s.pending != "" {
		line, s.pending = s.pending, ""
		return line, false, nil
	}
	select {
	case line = <-s.lines:
		return line, false, nil
	case <-s.readErr:
		return "", false, errMenuStdinClosed
	case <-s.shutdown.Done():
		return "", true, nil
	}
}

// readPrompt prints a prompt and returns the next line. It exists so the
// submenu writes its prompt through the same path the outer menu uses, keeping
// the printed and consumed line in step.
func (s *menuSource) readPrompt(prompt string, out io.Writer) (line string, stop bool, err error) {
	fmt.Fprint(out, prompt)
	return s.readLine()
}

// flushPending drains one line that arrived while no prompt was active, keeping
// it for the next prompt instead of blocking on it. It returns true when a line
// was captured or stdin ended.
func (s *menuSource) flushPending() {
	if s.pending != "" {
		return
	}
	select {
	case line := <-s.lines:
		s.pending = line
	default:
	}
}

// stopping reports whether a shutdown request has arrived.
func (s *menuSource) stopping() bool {
	select {
	case <-s.shutdown.Done():
		return true
	default:
		return false
	}
}

// runInteractiveMenu drives the TTY menu loop. Returning nil means "stop the
// server gracefully" (choice 4 or a stop request); a non-nil error means stdin
// closed or failed, so the caller falls back to plain signal babysitting.
//
// The reader goroutine is left parked on ReadString when we return — it exits
// with the process, and the alternative (closing stdin under the reader) would
// be worse.
func runInteractiveMenu(opts launcherOptions) error {
	out := opts.Out
	src := newMenuSource(opts.In, opts.shutdownOrIdle())

	for {
		if src.stopping() {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Gateway stopped.")
			return nil
		}

		fmt.Fprint(out, menuText())
		fmt.Fprint(out, "Select [1-4]: ")

		line, stop, err := src.readLine()
		if err != nil {
			return err
		}
		if stop {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Shutting down the gateway...")
			return nil
		}

		switch parseMenuChoice(line) {
		case menuChoiceOpenWeb:
			openDashboard(dashboardURL(opts.Port), out)
		case menuChoiceLogs:
			fmt.Fprintln(out)
			fmt.Fprintln(out, "The gateway logs stream in this terminal, above this menu.")
			fmt.Fprintf(out, "Server: %s\n", serverURL(opts.Port))
			fmt.Fprintf(out, "Dashboard: %s\n", dashboardURL(opts.Port))
			fmt.Fprintf(out, "Health: %s\n", healthURL(opts.Port))
		case menuChoicePersona:
			// The submenu runs in-process against the shared database. Stored
			// personas are read fresh on entry, so an edit made in the dashboard
			// meanwhile is visible without a restart. It reads through the same
			// source, and returns stop=true if a stop request arrives while it is
			// open.
			if stop := runPersonaSubmenu(opts, src, out); stop {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "Shutting down the gateway...")
				return nil
			}
		case menuChoiceExit:
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Shutting down the gateway...")
			opts.shutdownOrIdle().Request()
			return nil
		default:
			fmt.Fprintln(out, "Please choose 1, 2, 3 or 4.")
		}
		// A keystroke typed while a choice was being handled must not be lost:
		// keep one line aside for the next prompt.
		if !src.stopping() {
			src.flushPending()
		}
	}
}

// runTTYLauncher prints the ready banner and runs the menu until the user exits.
// It never opens a browser by itself: the printed Dashboard URL is how the user
// reaches the UI, and menu choice 1 is the only thing that launches one. It
// returns the launcher error (if any) so the caller can decide between graceful
// shutdown and fallback babysitting.
func runTTYLauncher(opts launcherOptions, version string) error {
	fmt.Fprint(opts.Out, banner(version, opts.Port))
	return runInteractiveMenu(opts)
}

// applyLauncherEnv turns explicit launcher flags into environment variables
// before config is loaded. Viper's AutomaticEnv only sees the process
// environment, so this is the same single config path every other setting uses
// (no second config mechanism).
func applyLauncherEnv(port int, host string) {
	if port > 0 {
		os.Setenv("PORT", strconv.Itoa(port))
	}
	if host != "" {
		os.Setenv("HOST", host)
	}
}
