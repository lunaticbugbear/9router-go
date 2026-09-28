package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/db"
)

// TestRequireDashboardPage_TunnelGateVariesPeerAndHost pins the /dashboard half
// of C2. The tunnel page gate keyed off r.Host, so a caller reaching the
// gateway through the tunnel sent `Host: localhost`, the authority stopped
// matching the configured tunnel hostname, and /dashboard was served instead of
// redirected to /login. Peer and Host are varied independently here.
func TestRequireDashboardPage_TunnelGateVariesPeerAndHost(t *testing.T) {
	t.Setenv("JWT_SECRET", "middleware-test-secret")
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_CLOUDFLARE", "")
	t.Setenv("NINEROUTER_PEER_TOKEN", "")
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)

	if err := repo.UpdateSettingsRaw(map[string]any{
		"tunnelUrl": "https://tunnel.example.com",
	}); err != nil {
		t.Fatalf("seed tunnel: %v", err)
	}

	page := RequireDashboardPage(repo)(okHandler())

	// A valid session isolates the tunnel gate from the session gate: without
	// one, both arms redirect to /login and the assertion could not tell them
	// apart.
	token, err := auth.Sign(auth.Secret(), time.Now())
	if err != nil {
		t.Fatalf("sign session: %v", err)
	}

	serve := func(remoteAddr, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req.RemoteAddr = remoteAddr
		req.Host = host
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		rec := httptest.NewRecorder()
		page.ServeHTTP(rec, req)
		return rec
	}

	t.Run("remote peer cannot reach /dashboard with a loopback Host", func(t *testing.T) {
		for _, host := range []string{"localhost", "LOCALHOST", "LocalHost", "127.0.0.1", "localhost:80", "[::1]:20130"} {
			rec := serve("203.0.113.10:1234", host)
			if rec.Code != http.StatusFound {
				t.Errorf("Host %q: expected 302 to /login, got %d", host, rec.Code)
				continue
			}
			if loc := rec.Header().Get("Location"); loc != "/login" {
				t.Errorf("Host %q: expected Location /login, got %q", host, loc)
			}
		}
	})

	t.Run("remote peer naming the tunnel host is redirected", func(t *testing.T) {
		rec := serve("203.0.113.10:1234", "tunnel.example.com")
		if rec.Code != http.StatusFound {
			t.Errorf("expected 302 to /login, got %d", rec.Code)
		}
	})

	t.Run("loopback peer with a loopback Host is served", func(t *testing.T) {
		for _, host := range []string{"localhost", "localhost:20130", "127.0.0.1:20130"} {
			rec := serve("127.0.0.1:1234", host)
			if rec.Code != http.StatusOK {
				t.Errorf("Host %q: local browser must be served, got %d", host, rec.Code)
			}
		}
	})
}

// TestRequireDashboardPage_TunnelAccessEnabled pins that enabling tunnel
// dashboard access leaves the documented behaviour intact: the dashboard is
// reachable through the tunnel with login still required, so the redirect comes
// from the session gate rather than the tunnel gate. The session gate and the
// tunnel gate both redirect to /login, so this asserts the observable contract
// (a redirect, never a 200 without a session) rather than which arm fired.
func TestRequireDashboardPage_TunnelAccessEnabled(t *testing.T) {
	t.Setenv("JWT_SECRET", "middleware-test-secret")
	t.Setenv("DATA_DIR", t.TempDir())
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)

	if err := repo.UpdateSettingsRaw(map[string]any{
		"tunnelUrl":             "https://tunnel.example.com",
		"tunnelDashboardAccess": true,
	}); err != nil {
		t.Fatalf("seed tunnel: %v", err)
	}

	page := RequireDashboardPage(repo)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Host = "tunnel.example.com"
	rec := httptest.NewRecorder()
	page.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("unauthenticated tunnel navigation must still be redirected, got %d", rec.Code)
	}
}
