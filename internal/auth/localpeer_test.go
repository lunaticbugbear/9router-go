package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":            true,
		"127.0.0.1:20130":      true,
		"127.0.0.5":            true, // whole 127/8 is loopback
		"::1":                  true,
		"[::1]":                true,
		"[::1]:20130":          true,
		"::ffff:127.0.0.1":     true, // dual-stack 4-in-6 form
		"localhost":            true,
		"LOCALHOST":            true,
		"localhost:80":         true,
		"::1%lo0":              true, // zoned IPv6
		"":                     false,
		"10.62.89.193":         false,
		"10.62.89.193:20130":   false,
		"192.0.2.1:1234":       false,
		"203.0.113.10":         false,
		"tunnel.example.com":   false,
		"127.0.0.1.evil.test":  false,
		"localhost.evil.test":  false,
		"fe80::1":              false,
		"0.0.0.0":              false, // unspecified is not loopback
		"not-an-address":       false,
		"127.0.0.1:not-a-port": false,
	}
	for addr, want := range cases {
		if got := IsLoopbackAddr(addr); got != want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestIsLocalRequestVariesPeerAndHostIndependently is the regression test for
// the Host-header bypass. The pre-fix helper consulted r.Host first and
// returned true for a loopback Host, so a remote caller became "local" by
// sending `Host: localhost`. The two inputs must be varied independently,
// because the old test suite only ever set them to consistent values.
func TestIsLocalRequestVariesPeerAndHostIndependently(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("TRUST_CLOUDFLARE", "")
	t.Setenv("NINEROUTER_PEER_TOKEN", "")

	loopbackHosts := []string{"localhost", "LOCALHOST", "LocalHost", "127.0.0.1", "localhost:80", "[::1]:20130"}

	t.Run("remote peer cannot become local with a loopback Host", func(t *testing.T) {
		for _, host := range loopbackHosts {
			req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
			req.RemoteAddr = "203.0.113.10:1234"
			req.Host = host
			if IsLocalRequest(req) {
				t.Errorf("remote peer with Host %q must not be local", host)
			}
		}
	})

	t.Run("loopback peer stays local whatever Host it sends", func(t *testing.T) {
		for _, host := range []string{"localhost", "203.0.113.10:20130", "tunnel.example.com", "10.62.89.193:20130"} {
			req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			req.Host = host
			if !IsLocalRequest(req) {
				t.Errorf("loopback peer with Host %q must be local", host)
			}
		}
	})

	t.Run("loopback peer in IPv6 and 4-in-6 forms", func(t *testing.T) {
		for _, peer := range []string{"[::1]:1234", "::1", "::ffff:127.0.0.1:1234"} {
			req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
			req.RemoteAddr = peer
			req.Host = "203.0.113.10:20130"
			if !IsLocalRequest(req) {
				t.Errorf("loopback peer %q must be local", peer)
			}
		}
	})

	t.Run("remote peer with honest Host stays remote", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "203.0.113.10:20130"
		if IsLocalRequest(req) {
			t.Error("remote peer with an honest Host must stay remote")
		}
	})
}

// TestIsLocalRequestRejectsRemoteOriginFromLoopbackPeer pins the second half of
// upstream isLocalRequest: a page served from a remote origin inside the
// operator's own browser can still reach the loopback listener, so a
// non-loopback Origin disqualifies the request.
func TestIsLocalRequestRejectsRemoteOriginFromLoopbackPeer(t *testing.T) {
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("NINEROUTER_PEER_TOKEN", "")

	cases := map[string]bool{
		"":                            true,
		"http://localhost:20130":      true,
		"http://127.0.0.1:20130":      true,
		"https://evil.example.com":    false,
		"http://192.0.2.7:20130":      false,
		"not a url":                   false,
		"https://localhost.evil.test": false,
	}
	for origin, want := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Host = "localhost:20130"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if got := IsLocalRequest(req); got != want {
			t.Errorf("Origin %q: IsLocalRequest = %v, want %v", origin, got, want)
		}
	}
}

// TestIsLocalRequestTrustedProxy pins the forwarded-address path: a forwarded
// client address is honoured only when an existing trust configuration
// authorises it, and it must never widen the local set otherwise.
func TestIsLocalRequestTrustedProxy(t *testing.T) {
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", nil)
		req.RemoteAddr = "203.0.113.10:1234" // the proxy hop, not the end user
		req.Host = "localhost:20130"
		return req
	}

	t.Run("untrusted forwarded header cannot widen the local set", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := newReq()
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		req.Header.Set("x-9r-real-ip", "127.0.0.1")
		if IsLocalRequest(req) {
			t.Error("an untrusted forwarded address must not make a remote peer local")
		}
	})

	t.Run("TRUST_PROXY honours a loopback XFF", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := newReq()
		req.Header.Set("X-Forwarded-For", "127.0.0.1, 10.0.0.1")
		if !IsLocalRequest(req) {
			t.Error("TRUST_PROXY with a loopback client address must be local")
		}
	})

	t.Run("TRUST_PROXY with a remote client address stays remote", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := newReq()
		req.Header.Set("X-Forwarded-For", "198.51.100.7")
		if IsLocalRequest(req) {
			t.Error("TRUST_PROXY with a remote client address must stay remote")
		}
	})

	t.Run("TRUST_CLOUDFLARE honours CF-Connecting-IP", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		req := newReq()
		req.Header.Set("CF-Connecting-IP", "127.0.0.1")
		if !IsLocalRequest(req) {
			t.Error("TRUST_CLOUDFLARE with a loopback client address must be local")
		}
	})

	t.Run("peer proof honours a loopback real-ip", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "peer-secret")
		req := newReq()
		req.Header.Set(PeerTokenHeader, "peer-secret")
		req.Header.Set(PeerRealIPHeader, "127.0.0.1")
		if !IsLocalRequest(req) {
			t.Error("a proven peer real-ip of loopback must be local")
		}
	})

	t.Run("peer real-ip without the proof is ignored", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "peer-secret")
		req := newReq()
		req.Header.Set(PeerRealIPHeader, "127.0.0.1")
		if IsLocalRequest(req) {
			t.Error("x-9r-real-ip without the peer token must not be believed")
		}
	})
}
