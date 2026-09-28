package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	json "encoding/json/v2"
)

// postValidateNodePeerHost posts a provider-node validation with the peer
// address and the client-supplied Host set independently. The pre-fix SSRF
// gate consulted Host first, so a remote caller could skip AssertPublicURL by
// sending `Host: localhost` and reach an internal listener with the stored
// credential attached.
func postValidateNodePeerHost(t *testing.T, body, remoteAddr, host string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	router := setupTestRouter(repo)

	req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

// TestValidateProviderNode_SSRFGuardCannotBeSkippedWithSpoofedHost is the C3
// regression test: the SSRF guard must run for a remote caller no matter which
// Host it claims, and a genuine loopback caller must still reach a self-hosted
// node.
func TestValidateProviderNode_SSRFGuardCannotBeSkippedWithSpoofedHost(t *testing.T) {
	const internal = `{"baseUrl":"http://127.0.0.1:20196/v1","apiKey":"sk-synthetic","type":"openai-compatible"}`

	t.Run("remote peer cannot skip the guard with a loopback Host", func(t *testing.T) {
		for _, host := range []string{"localhost", "LOCALHOST", "LocalHost", "localhost:80", "127.0.0.1", "[::1]:20130"} {
			calls := staticProbeStub(t, http.StatusOK)
			rec, out := postValidateNodePeerHost(t, internal, "203.0.113.10:1234", host)
			if rec.Code != http.StatusOK {
				t.Errorf("Host %q: expected 200, got %d", host, rec.Code)
			}
			if got := nodeOut(t, out, "error"); got != "URL not allowed" {
				t.Errorf("Host %q: remote caller must be SSRF-blocked, got %q", host, got)
			}
			if nodeValid(t, out) {
				t.Errorf("Host %q: blocked URL must not validate", host)
			}
			if len(*calls) != 0 {
				t.Errorf("Host %q: blocked URL must not be probed (%d probes)", host, len(*calls))
			}
		}
	})

	t.Run("remote peer with an honest Host is blocked", func(t *testing.T) {
		calls := staticProbeStub(t, http.StatusOK)
		rec, out := postValidateNodePeerHost(t, internal, "203.0.113.10:1234", "203.0.113.10:20130")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if got := nodeOut(t, out, "error"); got != "URL not allowed" {
			t.Errorf("expected URL not allowed, got %q", got)
		}
		if len(*calls) != 0 {
			t.Errorf("expected no probe for a blocked URL, got %d", len(*calls))
		}
	})

	t.Run("loopback peer reaches a self-hosted node whatever Host it sends", func(t *testing.T) {
		for _, host := range []string{"localhost:20130", "203.0.113.10:20130", "10.62.89.193:20130"} {
			calls := staticProbeStub(t, http.StatusOK)
			rec, out := postValidateNodePeerHost(t, internal, "127.0.0.1:1234", host)
			if rec.Code != http.StatusOK {
				t.Errorf("Host %q: expected 200, got %d", host, rec.Code)
			}
			if !nodeValid(t, out) {
				t.Errorf("Host %q: self-hosted node should validate locally, got %s", host, rec.Body.String())
			}
			if len(*calls) == 0 {
				t.Errorf("Host %q: expected a models probe for the self-hosted node", host)
			}
		}
	})

	t.Run("an untrusted forwarded address cannot skip the guard", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "")
		t.Setenv("TRUST_CLOUDFLARE", "")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		calls := staticProbeStub(t, http.StatusOK)
		req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", bytes.NewReader([]byte(internal)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:1234"
		req.Host = "localhost"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		repo, cleanup := setupTestDB(t)
		defer cleanup()
		rec := httptest.NewRecorder()
		setupTestRouter(repo).ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if got := nodeOut(t, out, "error"); got != "URL not allowed" {
			t.Errorf("an untrusted XFF must not skip the SSRF guard, got %q", got)
		}
		if len(*calls) != 0 {
			t.Errorf("expected no probe, got %d", len(*calls))
		}
	})

	t.Run("a trusted proxy forwarding a loopback client may skip the guard", func(t *testing.T) {
		t.Setenv("TRUST_PROXY", "true")
		t.Setenv("NINEROUTER_PEER_TOKEN", "")
		calls := staticProbeStub(t, http.StatusOK)
		req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes/validate", bytes.NewReader([]byte(internal)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234" // the proxy hop
		req.Host = "localhost:20130"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		repo, cleanup := setupTestDB(t)
		defer cleanup()
		rec := httptest.NewRecorder()
		setupTestRouter(repo).ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if !nodeValid(t, out) {
			t.Errorf("trusted loopback client should reach the node, got %s", rec.Body.String())
		}
		if len(*calls) == 0 {
			t.Error("expected a probe for the trusted local client")
		}
	})
}
