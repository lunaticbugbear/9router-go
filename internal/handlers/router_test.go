package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/db"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpFile, err := os.CreateTemp("", "test_router_*.sqlite")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	database, err := db.OpenDatabase(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("OpenDatabase failed: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.Remove(tmpFile.Name())
	}
	return database, cleanup
}

func TestSetupRoutes(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupRoutes(r, repo, nil)

	req := httptest.NewRequest("POST", "/chat/completions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusMethodNotAllowed || w.Code == http.StatusNotFound {
		t.Errorf("expected /chat/completions route to be registered, got status %d", w.Code)
	}
}

func TestSetupServerRouter_ModelTestAuthDomains(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK (id = 1), data TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings table: %v", err)
	}

	repo := db.NewRepo(database)
	if err := repo.UpdateSettingsRaw(map[string]any{"requireLogin": false}); err != nil {
		t.Fatalf("disable dashboard login for route test: %v", err)
	}

	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	dashboardRec := httptest.NewRecorder()
	r.ServeHTTP(dashboardRec, httptest.NewRequest(http.MethodPost, "/api/dashboard/models/test", nil))
	if dashboardRec.Code != http.StatusBadRequest {
		t.Fatalf("dashboard route should reach handler without client key; got %d: %s", dashboardRec.Code, dashboardRec.Body.String())
	}

	clientRec := httptest.NewRecorder()
	r.ServeHTTP(clientRec, httptest.NewRequest(http.MethodPost, "/api/models/test", nil))
	if clientRec.Code != http.StatusUnauthorized {
		t.Fatalf("client route must remain API-key protected; got %d: %s", clientRec.Code, clientRec.Body.String())
	}
}

func TestSetupServerRouter_DashboardSessionReachesConsoleAndVersion(t *testing.T) {
	t.Setenv("JWT_SECRET", "router-session-test-secret")
	t.Setenv("DATA_DIR", t.TempDir())
	database, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := database.Exec(`CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK (id = 1), data TEXT NOT NULL)`); err != nil {
		t.Fatalf("create settings table: %v", err)
	}
	repo := db.NewRepo(database)
	if err := repo.UpdateSettingsRaw(map[string]any{"requireLogin": true}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	token, err := auth.Sign(auth.Secret(), time.Now())
	if err != nil {
		t.Fatalf("sign session: %v", err)
	}

	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	for _, path := range []string{"/translator/console-logs/level", "/api/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		// The dashboard also sends a stale client bearer; the session must win.
		req.Header.Set("Authorization", "Bearer sk-not-a-stored-key")
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s with dashboard session: got %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSetupRoutes_OAuthEndpointsMounted(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupRoutes(r, repo, nil)

	endpoints := []struct {
		method string
		path   string
	}{
		{"POST", "/api/oauth/freebuff/initiate"},
		{"POST", "/api/oauth/freebuff/poll"},
		{"GET", "/api/oauth/freebuff/session"},
		{"POST", "/api/oauth/freebuff/session/switch"},
		{"GET", "/api/oauth/antigravity/authorize"},
		{"GET", "/api/oauth/antigravity/callback"},
		{"POST", "/api/oauth/antigravity/callback"},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("expected %s %s route to be registered, got 404", ep.method, ep.path)
		}
	}
}

func TestSetupServerRouter_PprofMounted(t *testing.T) {
	t.Setenv("PPROF_ENABLED", "true")
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/goroutine"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected %s to return 200 OK when PPROF_ENABLED=true, got %d", path, w.Code)
		}
	}
}

func TestSetupServerRouter_PprofDisabledByDefault(t *testing.T) {
	t.Setenv("PPROF_ENABLED", "false")
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/profile"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected %s to return 404 Not Found by default, got %d", path, w.Code)
		}
	}
}

func TestSetupServerRouter_SPARoutes(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	repo := db.NewRepo(database)
	r := chi.NewRouter()
	SetupServerRouter(r, repo, nil)

	// Unauthenticated with no settings row: login is required, so /dashboard
	// pages redirect to /login (upstream dashboardGuard) while the other SPA
	// aliases still serve the shell (the SPA shows the login screen itself).
	guardedPaths := []string{
		"/dashboard",
		"/dashboard/combos",
		"/dashboard/providers",
		"/dashboard/terminal",
		"/dashboard/usage",
		"/dashboard/quota",
	}
	for _, p := range guardedPaths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("expected GET %s to redirect to /login, got %d", p, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/login" {
			t.Errorf("expected GET %s Location /login, got %q", p, loc)
		}
	}

	spaPaths := []string{
		"/login",
		"/connections",
		"/combos",
		"/analytics",
		"/terminal",
		"/keys",
		"/settings",
		"/providers",
		"/usage",
		"/quota",
	}
	for _, p := range spaPaths {
		req := httptest.NewRequest("GET", p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected GET %s to return 200, got %d (Location: %s): %s", p, w.Code, w.Header().Get("Location"), w.Body.String())
		}
	}

	// Ensure static assets work
	reqAsset := httptest.NewRequest("GET", "/providers/anthropic.png", nil)
	wAsset := httptest.NewRecorder()
	r.ServeHTTP(wAsset, reqAsset)
	if wAsset.Code != http.StatusOK {
		t.Errorf("expected GET /providers/anthropic.png to return 200, got %d", wAsset.Code)
	}

	// Ensure non-existent static assets return 404
	reqMissing := httptest.NewRequest("GET", "/assets/missing.js", nil)
	wMissing := httptest.NewRecorder()
	r.ServeHTTP(wMissing, reqMissing)
	if wMissing.Code != http.StatusNotFound {
		t.Errorf("expected GET /assets/missing.js to return 404, got %d", wMissing.Code)
	}

	// Ensure API endpoints like /api/settings are NOT shadowed by SPA handler
	reqAPI := httptest.NewRequest("GET", "/api/settings", nil)
	wAPI := httptest.NewRecorder()
	r.ServeHTTP(wAPI, reqAPI)
	// When no API keys exist in test DB, RequireApiKey allows or denies based on settings
	if wAPI.Code == http.StatusNotFound {
		t.Errorf("expected /api/settings to be handled by API handler, not 404")
	}
}
