package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy"
)

func TestForwardGitHubResponsesFallback(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"unsupported_api_for_model","message":"use responses"}}`)
		case "/responses":
			if !strings.Contains(string(body), `"input"`) || !strings.Contains(string(body), `"model":"gpt-5.3-codex"`) {
				t.Errorf("expected translated Responses request, got %s", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n")
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	req := &Request{Ctx: context.Background(), Client: server.Client(),
		Config: &providers.ProviderConfig{BaseURL: server.URL + "/chat/completions"},
		Body:   []byte(`{"model":"gpt-5.3-codex","messages":[{"role":"user","content":"hi"}]}`),
	}
	rec := httptest.NewRecorder()
	if err := ForwardGitHub(rec, req); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "/chat/completions,/responses" || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("paths=%v status=%d body=%s", paths, rec.Code, rec.Body.String())
	}
}

func TestForwardGitHubDoesNotRetryUnsupportedModel(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"model_not_supported","message":"not available"}}`)
	}))
	defer server.Close()
	req := &Request{Ctx: context.Background(), Client: server.Client(),
		Config:    &providers.ProviderConfig{BaseURL: server.URL + "/chat/completions"},
		ModelName: "old-model", Body: []byte(`{"model":"old-model","messages":[{"role":"user","content":"hi"}]}`),
	}
	err := ForwardGitHub(httptest.NewRecorder(), req)
	var upstream *proxy.UpstreamError
	if !errors.As(err, &upstream) || requests != 1 {
		t.Errorf("expected one request and upstream error, requests=%d err=%v", requests, err)
	}
}

func TestForwardGitHubDoesNotRetryClaudeOnResponses(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"unsupported_api_for_model"}}`)
	}))
	defer server.Close()
	req := &Request{Ctx: context.Background(), Client: server.Client(),
		Config: &providers.ProviderConfig{BaseURL: server.URL + "/chat/completions"},
		Body:   []byte(`{"model":"claude-opus-4.5","messages":[{"role":"user","content":"hi"}]}`),
	}
	if err := ForwardGitHub(httptest.NewRecorder(), req); err == nil || requests != 1 {
		t.Errorf("Claude must not be retried on Responses: requests=%d err=%v", requests, err)
	}
}
