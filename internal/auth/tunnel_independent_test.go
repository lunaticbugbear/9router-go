package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// tunnelRaw is the settings shape for a configured tunnel with dashboard access
// disabled (the exploitable configuration).
func tunnelRaw() map[string]any {
	return map[string]any{"tunnelUrl": "https://tunnel.example.com"}
}

// TestTunnelLoginBlockedVariesPeerAndHostIndependently is the regression test
// for C2. The gate keyed off r.Host alone, so a caller reaching the gateway
// through the tunnel sent `Host: localhost`, the authority stopped matching the
// configured tunnel hostname, and the gate was skipped. Peer and Host are
// varied independently here.
func TestTunnelLoginBlockedVariesPeerAndHostIndependently(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_CLOUDFLARE", "")
	t.Setenv("NINEROUTER_PEER_TOKEN", "")

	raw := tunnelRaw()

	t.Run("remote peer claiming a loopback Host is still blocked", func(t *testing.T) {
		for _, host := range []string{"localhost", "LOCALHOST", "LocalHost", "127.0.0.1", "localhost:80", "[::1]:20130"} {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			req.RemoteAddr = "203.0.113.10:1234"
			req.Host = host
			if !TunnelLoginBlocked(req, raw) {
				t.Errorf("remote peer with Host %q must be tunnel-blocked", host)
			}
		}
	})

	t.Run("remote peer naming the tunnel host is blocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "tunnel.example.com"
		if !TunnelLoginBlocked(req, raw) {
			t.Error("honest tunnel arrival must be blocked")
		}
	})

	t.Run("loopback peer is not blocked", func(t *testing.T) {
		for _, host := range []string{"localhost", "localhost:20130", "127.0.0.1:20130"} {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			req.Host = host
			if TunnelLoginBlocked(req, raw) {
				t.Errorf("loopback peer with Host %q must not be blocked", host)
			}
		}
	})

	t.Run("loopback peer with a remote Origin is not a local browser", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Host = "localhost:20130"
		req.Header.Set("Origin", "https://evil.example.com")
		if !TunnelLoginBlocked(req, raw) {
			t.Error("a non-loopback Origin must not read as a local browser")
		}
	})

	t.Run("explicit tunnel access disables the gate", func(t *testing.T) {
		enabled := map[string]any{
			"tunnelUrl":             "https://tunnel.example.com",
			"tunnelDashboardAccess": true,
		}
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "localhost"
		if TunnelLoginBlocked(req, enabled) {
			t.Error("tunnelDashboardAccess=true must disable the gate")
		}
	})

	t.Run("no tunnel configured leaves everyone alone", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "localhost"
		if TunnelLoginBlocked(req, map[string]any{}) {
			t.Error("no configured tunnel means the gate is inert")
		}
	})

	t.Run("an honest LAN authority is not tunnel-blocked", func(t *testing.T) {
		// Disabling tunnel access says nothing about LAN access, which stays
		// governed by requireLogin. Blocking this would strand operators who use
		// both.
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "10.62.89.50:1234"
		req.Host = "10.62.89.193:20130"
		if TunnelLoginBlocked(req, raw) {
			t.Error("an honest LAN caller must not be tunnel-blocked")
		}
	})

	t.Run("tailscaleUrl is gated too", func(t *testing.T) {
		ts := map[string]any{"tailscaleUrl": "https://host.tailnet.ts.net"}
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "host.tailnet.ts.net"
		if !TunnelLoginBlocked(req, ts) {
			t.Error("tailscale hostname must be blocked")
		}
	})
}

// TestTunnelLoginBlockedTrustedProxy pins the forwarded-address interaction:
// a front door that forwards the real client address makes the caller
// non-loopback, so the spoofed-loopback arm applies.
func TestTunnelLoginBlockedTrustedProxy(t *testing.T) {
	raw := tunnelRaw()

	t.Run("trusted proxy forwarding a remote client is blocked", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "127.0.0.1:1234" // the proxy hop
		req.Host = "localhost"
		req.Header.Set("X-Forwarded-For", "198.51.100.7")
		if !TunnelLoginBlocked(req, raw) {
			t.Error("forwarded remote client must be tunnel-blocked")
		}
	})

	t.Run("trusted proxy forwarding a loopback client is allowed", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Host = "localhost"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		if TunnelLoginBlocked(req, raw) {
			t.Error("forwarded loopback client must not be tunnel-blocked")
		}
	})

	t.Run("an untrusted XFF cannot waive the gate", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "localhost"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		if !TunnelLoginBlocked(req, raw) {
			t.Error("an untrusted XFF must not turn a remote caller into a local one")
		}
	})
}
