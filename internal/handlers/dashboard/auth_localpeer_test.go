package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	json "encoding/json/v2"

	"9router/proxy/internal/auth"
)

// loginWithPeerAndHost drives HandleAuthLogin with the peer address and the
// client-supplied Host set independently. The pre-fix guard consulted Host
// first, so these two had to vary separately to expose the bypass — the
// existing remote-default-password test only ever set them to consistent
// remote values.
func loginWithPeerAndHost(t *testing.T, h *DashboardHandler, password, remoteAddr, host string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"password":` + mustJSON(t, password) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(body)))
	req.RemoteAddr = remoteAddr
	req.Host = host
	rec := httptest.NewRecorder()
	h.HandleAuthLogin(rec, req)
	return rec
}

// hasSessionCookie reports whether the response issued a non-empty session
// cookie.
func hasSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return true
		}
	}
	return false
}

// TestHandleAuthLogin_RemoteDefaultPasswordCannotSpoofHost is the C1 regression
// test: a remote caller must not defeat the fresh-install guard by claiming a
// loopback Host. Every accepted spelling of the trick is covered, and the
// inverse (a genuine loopback caller sending a remote Host) must still work.
func TestHandleAuthLogin_RemoteDefaultPasswordCannotSpoofHost(t *testing.T) {
	authTestEnv(t)
	auth.ResetLoginLimiter()
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	t.Run("remote peer with a spoofed loopback Host is refused", func(t *testing.T) {
		for _, host := range []string{"localhost", "LOCALHOST", "LocalHost", "localhost:80", "127.0.0.1", "127.0.0.1:20130", "[::1]:20130"} {
			auth.ResetLoginLimiter()
			rec := loginWithPeerAndHost(t, h, "123456", "203.0.113.10:1234", host)
			if rec.Code != http.StatusForbidden {
				t.Errorf("Host %q: expected 403, got %d: %s", host, rec.Code, rec.Body.String())
				continue
			}
			if hasSessionCookie(t, rec) {
				t.Errorf("Host %q: refused login must not issue a session cookie", host)
			}
			var out map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Errorf("Host %q: decode: %v", host, err)
				continue
			}
			if out["mustChangePassword"] != true {
				t.Errorf("Host %q: expected mustChangePassword=true, got %v", host, out["mustChangePassword"])
			}
		}
	})

	t.Run("loopback peer with a remote Host is local and allowed", func(t *testing.T) {
		for _, host := range []string{"203.0.113.10:20130", "10.62.89.193:20130", "tunnel.example.com"} {
			auth.ResetLoginLimiter()
			rec := loginWithPeerAndHost(t, h, "123456", "127.0.0.1:1234", host)
			if rec.Code != http.StatusOK {
				t.Errorf("Host %q: loopback peer must be allowed, got %d: %s", host, rec.Code, rec.Body.String())
				continue
			}
			if !hasSessionCookie(t, rec) {
				t.Errorf("Host %q: loopback login must issue a session cookie", host)
			}
		}
	})

	t.Run("IPv6 and 4-in-6 loopback peers are local", func(t *testing.T) {
		for _, peer := range []string{"[::1]:1234", "::ffff:127.0.0.1:1234"} {
			auth.ResetLoginLimiter()
			rec := loginWithPeerAndHost(t, h, "123456", peer, "203.0.113.10:20130")
			if rec.Code != http.StatusOK {
				t.Errorf("peer %q: expected 200, got %d: %s", peer, rec.Code, rec.Body.String())
			}
		}
	})
}

// TestHandleAuthLogin_TrustedProxyForwardedAddress pins the forwarded-address
// path for the login gate: only an existing trust configuration may supply the
// client address, and an untrusted header can never widen the local set.
func TestHandleAuthLogin_TrustedProxyForwardedAddress(t *testing.T) {
	authTestEnv(t)
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	loginViaProxy := func(t *testing.T, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		body := `{"password":"123456"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(body)))
		req.RemoteAddr = "127.0.0.1:1234" // the proxy hop, not the end user
		req.Host = "localhost:20130"
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.HandleAuthLogin(rec, req)
		return rec
	}

	t.Run("untrusted forwarded address is ignored", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		auth.ResetLoginLimiter()
		// The socket is loopback, so the caller reads as local; the untrusted
		// header must not change that in either direction.
		if rec := loginViaProxy(t, map[string]string{"X-Forwarded-For": "198.51.100.7"}); rec.Code != http.StatusOK {
			t.Errorf("untrusted XFF must not override the loopback peer, got %d", rec.Code)
		}
	})

	t.Run("TRUST_PROXY remote client is refused", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		auth.ResetLoginLimiter()
		rec := loginViaProxy(t, map[string]string{"X-Forwarded-For": "198.51.100.7"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("forwarded remote client must be refused, got %d: %s", rec.Code, rec.Body.String())
		}
		if hasSessionCookie(t, rec) {
			t.Error("refused proxy login must not issue a session cookie")
		}
	})

	t.Run("TRUST_PROXY loopback client is allowed", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		auth.ResetLoginLimiter()
		rec := loginViaProxy(t, map[string]string{"X-Forwarded-For": "127.0.0.1"})
		if rec.Code != http.StatusOK {
			t.Errorf("forwarded loopback client must be allowed, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
