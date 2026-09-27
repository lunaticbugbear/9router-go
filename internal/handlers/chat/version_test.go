package chat

import (
	"bytes"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/updater"
)

func TestHandleVersion(t *testing.T) {
	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("GET", "/api/version", nil)
	rec := httptest.NewRecorder()

	handler.HandleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var info updater.UpdateInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if info.CurrentVersion == "" {
		t.Errorf("expected non-empty CurrentVersion")
	}
}

func TestHandleVersionStatus(t *testing.T) {
	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("GET", "/api/version/status", nil)
	rec := httptest.NewRecorder()

	handler.HandleVersionStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status updater.UpdaterStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if status.CurrentVersion == "" {
		t.Errorf("expected currentVersion")
	}
}

func TestHandleToggleAutoUpdate(t *testing.T) {
	t.Setenv("UPDATE_URL", "https://updates.example.test/manifest")
	t.Setenv("UPDATE_REPO", "")
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	handler := NewChatHandler(repo, nil)

	body := `{"enabled":true}`
	req := httptest.NewRequest("POST", "/api/version/auto-update", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleToggleAutoUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if !updater.IsAutoUpdateEnabled() {
		t.Errorf("expected updater.IsAutoUpdateEnabled to be true")
	}

	settings, err := repo.GetSettings()
	if err != nil || !settings.AutoUpdate {
		t.Errorf("expected settings.AutoUpdate to be true in DB, err=%v", err)
	}
}

func TestHandleToggleAutoUpdate_DisabledWithoutSource(t *testing.T) {
	t.Setenv("UPDATE_URL", "")
	t.Setenv("UPDATE_REPO", "")
	updater.SetAutoUpdate(true)

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/version/auto-update", strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	handler.HandleToggleAutoUpdate(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when no release source is configured, got %d", rec.Code)
	}
	if updater.IsAutoUpdateEnabled() {
		t.Error("auto-update must remain disabled without an explicit source")
	}
}

func TestHandleCheckUpdate_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manifest := map[string]any{
			"latestVersion": "9.9.9",
			"downloadUrl":   "https://example.com/download",
			"releaseNotes":  "Test release notes",
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, manifest)
	}))
	defer server.Close()

	os.Setenv("UPDATE_URL", server.URL)
	defer os.Unsetenv("UPDATE_URL")

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("GET", "/api/version/check", nil)
	rec := httptest.NewRecorder()

	handler.HandleCheckUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var info updater.UpdateInfo
	json.Unmarshal(rec.Body.Bytes(), &info)
	if info.LatestVersion != "9.9.9" || !info.HasUpdate {
		t.Errorf("expected latestVersion 9.9.9 and hasUpdate=true, got %v", info)
	}
}

func TestHandleTriggerUpdate_UpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manifest := map[string]any{
			"latestVersion": updater.CurrentVersion,
			"downloadUrl":   "https://example.com/download",
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, manifest)
	}))
	defer server.Close()

	os.Setenv("UPDATE_URL", server.URL)
	defer os.Unsetenv("UPDATE_URL")

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("POST", "/api/version/update", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()

	handler.HandleTriggerUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["status"] != "up_to_date" {
		t.Errorf("expected status 'up_to_date', got %v", res["status"])
	}
}

func TestHandleTriggerUpdate_DisabledWithoutSource(t *testing.T) {
	t.Setenv("UPDATE_URL", "")
	t.Setenv("UPDATE_REPO", "")

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/version/update", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.HandleTriggerUpdate(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when self-update is disabled, got %d: %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["status"] != "disabled" {
		t.Errorf("status = %v, want disabled", response["status"])
	}
}

func TestHandleChangelog(t *testing.T) {
	// The handler serves CHANGELOG.md from the working directory first. Run in a
	// temp dir holding a fixture so the assertion is deterministic: the old test
	// ran from the package dir, missed the relative paths, and passed only
	// because the network answered the raw.githubusercontent.com fallback.
	t.Chdir(t.TempDir())
	const fixture = "# Changelog\n\n- fixture entry served from the local file\n"
	if err := os.WriteFile("CHANGELOG.md", []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture CHANGELOG.md: %v", err)
	}

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("GET", "/api/changelog", nil)
	rec := httptest.NewRecorder()

	handler.HandleChangelog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from local CHANGELOG.md, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Changelog") {
		t.Errorf("expected response to contain 'Changelog', got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "fixture entry served from the local file") {
		t.Errorf("expected the local fixture body to be served, got: %s", rec.Body.String())
	}
}

// stubChangelogTransport answers changelog requests from a canned body and
// records the URLs that were requested. It lets the remote fallback branch be
// exercised without leaving the machine: HandleChangelog builds its own
// *http.Client, so the network is stubbed at http.DefaultTransport.
type stubChangelogTransport struct {
	bodies map[string]string
	urls   []string
}

func (s *stubChangelogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.urls = append(s.urls, req.URL.String())
	body, ok := s.bodies[req.URL.String()]
	status := http.StatusNotFound
	if ok {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/markdown; charset=utf-8"}},
		Request:    req,
	}, nil
}

func TestHandleChangelog_RemoteFallback(t *testing.T) {
	// An empty working dir means no local CHANGELOG.md is reachable, so the
	// handler must use its remote fallback (which the stub serves offline).
	t.Chdir(t.TempDir())

	const firstURL = "https://raw.githubusercontent.com/luqman-v1/9router-go/main/CHANGELOG.md"

	stub := &stubChangelogTransport{bodies: map[string]string{
		firstURL: "# Changelog\n\n- remote fallback entry\n",
	}}
	origTransport := http.DefaultTransport
	http.DefaultTransport = stub
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	handler := NewChatHandler(nil, nil)
	req := httptest.NewRequest("GET", "/api/changelog", nil)
	rec := httptest.NewRecorder()

	handler.HandleChangelog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from remote fallback, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "remote fallback entry") {
		t.Errorf("expected the stubbed remote body to be served, got: %s", rec.Body.String())
	}
	if len(stub.urls) == 0 {
		t.Fatal("expected the remote fallback to be requested")
	}
	if stub.urls[0] != firstURL {
		t.Errorf("expected the first request to be %s, got %s", firstURL, stub.urls[0])
	}
	if len(stub.urls) > 1 {
		t.Errorf("expected the first URL to satisfy the request, got requests: %v", stub.urls)
	}
}
