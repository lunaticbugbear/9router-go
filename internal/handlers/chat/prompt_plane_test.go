package chat

import (
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
)

func TestAttachBountyProfile_ExplicitRequestSelection(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetBountyProfile(bounty.Profile{
		ID: "h1-demo", Program: "Demo", InScope: []string{"api.demo.test"},
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx, err := h.attachPromptPlane(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := promptPlaneFromContext(ctx); ok {
		t.Fatal("a profile was applied without an explicit header")
	}

	selected := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selected.Header.Set(BountyProfileHeader, "h1-demo")
	ctx, err = h.attachPromptPlane(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := promptPlaneFromContext(ctx)
	if !ok || got.Bounty == nil || got.Bounty.ID != "h1-demo" {
		t.Fatalf("selected profile not attached: %+v, ok=%v", got, ok)
	}
}

func TestAttachBountyProfile_UnknownProfileRefused(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	h := NewChatHandler(db.NewRepo(database))
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set(BountyProfileHeader, "not-configured")
	if _, err := h.attachPromptPlane(context.Background(), r); err == nil || !strings.Contains(err.Error(), "unknown bounty profile") {
		t.Fatalf("unknown profile must fail clearly, got %v", err)
	}
}

func TestApplyPromptPlaneToBody_PreservesCallerAndUsesProtocolSystemField(t *testing.T) {
	profile := bounty.Profile{ID: "p", Program: "Demo", InScope: []string{"api.demo.test"}}

	openAI := []byte(`{"model":"m","messages":[{"role":"user","content":"analyze"}]}`)
	out, changed, err := ApplyPromptPlaneToBody(openAI, PromptPlane{Bounty: &profile}, PromptWireOpenAIChat)
	if err != nil {
		t.Fatalf("openai injection failed: %v", err)
	}
	if !changed {
		t.Fatal("expected bounty context to be injected")
	}
	var req map[string]any
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	messages := req["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" || !strings.Contains(messages[0].(map[string]any)["content"].(string), "api.demo.test") {
		t.Fatalf("scope not added as an OpenAI system message: %s", out)
	}
	if messages[1].(map[string]any)["content"] != "analyze" {
		t.Fatalf("caller message changed: %s", out)
	}

	claude := []byte(`{"model":"m","messages":[{"role":"user","content":"analyze"}]}`)
	out, changed, err = ApplyPromptPlaneToBody(claude, PromptPlane{Bounty: &profile}, PromptWireClaudeMessages)
	if err != nil {
		t.Fatalf("claude injection failed: %v", err)
	}
	if !changed {
		t.Fatal("expected Claude scope context to be injected")
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req["system"].(string), "api.demo.test") {
		t.Fatalf("Anthropic top-level system field not populated: %s", out)
	}
	if req["messages"].([]any)[0].(map[string]any)["role"] == "system" {
		t.Fatal("Anthropic Messages forbids system role inside messages[]")
	}
}

func TestHandleChatCompletions_ProfileContextSentButSelectorHeaderNotForwarded(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	var receivedBountyHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedBountyHeader = r.Header.Get(BountyProfileHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"analysis complete"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-bounty", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetBountyProfile(bounty.Profile{
		ID: "h1-demo", Program: "Demo program", ProgramURL: "https://hackerone.com/demo",
		InScope: []string{"api.demo.test"}, OutOfScope: []string{"billing.demo.test"},
		Rules: "No destructive testing.",
	}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"analyze this finding"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set(BountyProfileHeader, "h1-demo")
	w := httptest.NewRecorder()
	h.HandleChatCompletions(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("chat failed: HTTP %d %s", w.Code, w.Body.String())
	}

	if receivedBountyHeader != "" {
		t.Fatalf("selection header leaked upstream: %q", receivedBountyHeader)
	}
	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	messages := sent["messages"].([]any)
	if len(messages) < 2 || messages[0].(map[string]any)["role"] != "system" {
		t.Fatalf("outgoing system prompt did not receive scope context: %s", receivedBody)
	}
	system := messages[0].(map[string]any)["content"].(string)
	for _, want := range []string{"Demo program", "api.demo.test", "billing.demo.test", "provider safety policies"} {
		if !strings.Contains(system, want) {
			t.Errorf("outgoing system context missing %q", want)
		}
	}
	if got := messages[1].(map[string]any)["content"]; got != "analyze this finding" {
		t.Errorf("user message was modified: %#v", got)
	}
}

// A selected chat request whose body cannot carry the scope in messages[] must
// be rejected before any upstream call — including a body that only has a
// Responses-shaped input[], which Chat Completions would ignore.
func TestHandleChatCompletions_UninjectableSelectedBodyRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"input[] only (wrong protocol for chat)", `{"model":"deepseek/deepseek-chat","input":[{"role":"user","content":"hi"}]}`},
		{"messages not an array", `{"model":"deepseek/deepseek-chat","messages":"nope"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, cleanup := setupChatTestDB(t)
			defer cleanup()

			var calls int
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"ok"}}]}`))
			}))
			defer upstream.Close()

			seedConnDB(t, database, "deepseek", "conn-uninj", "sk-test", upstream.URL)
			repo := db.NewRepo(database)
			if err := repo.SetBountyProfile(bounty.Profile{
				ID: "h1-uninj", Program: "Demo", InScope: []string{"api.demo.test"},
			}); err != nil {
				t.Fatal(err)
			}

			h := NewChatHandler(repo)
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			r.Header.Set(BountyProfileHeader, "h1-uninj")
			w := httptest.NewRecorder()
			h.HandleChatCompletions(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for uninjectable selected body, got %d: %s", w.Code, w.Body.String())
			}
			if calls != 0 {
				t.Fatalf("rejected request must not reach upstream, got %d calls", calls)
			}
		})
	}
}

func TestBountyProfileIsNotImplicitOrStickyAcrossRequests(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var bodies [][]byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()
	seedConnDB(t, database, "deepseek", "conn-scope", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetBountyProfile(bounty.Profile{ID: "program-a", Program: "Program A", InScope: []string{"a.example"}}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"review"}]}`

	// First request explicitly selects the profile.
	selected := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	selected.Header.Set(BountyProfileHeader, "program-a")
	selectedRec := httptest.NewRecorder()
	h.HandleChatCompletions(selectedRec, selected)
	if selectedRec.Code != http.StatusOK {
		t.Fatalf("selected request failed: %d %s", selectedRec.Code, selectedRec.Body.String())
	}

	// Second request on the same handler has no profile header. The selected
	// profile must not stick to a connection or global handler state.
	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	plainRec := httptest.NewRecorder()
	h.HandleChatCompletions(plainRec, plain)
	if plainRec.Code != http.StatusOK {
		t.Fatalf("plain request failed: %d %s", plainRec.Code, plainRec.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(bodies))
	}

	var first, second map[string]any
	if err := json.Unmarshal(bodies[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bodies[1], &second); err != nil {
		t.Fatal(err)
	}
	firstMessages := first["messages"].([]any)
	firstSystem := firstMessages[0].(map[string]any)["content"].(string)
	if !strings.Contains(firstSystem, "a.example") {
		t.Fatalf("selected request did not receive profile scope: %s", bodies[0])
	}
	secondMessages := second["messages"].([]any)
	if len(secondMessages) != 1 || secondMessages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("unselected request inherited a previous profile: %s", bodies[1])
	}
}

func TestUnknownBountyProfileRejectsBeforeUpstream(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"unexpected"}}]}`))
	}))
	defer upstream.Close()
	seedConnDB(t, database, "deepseek", "conn-unknown-profile", "sk-test", upstream.URL)
	h := NewChatHandler(db.NewRepo(database))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set(BountyProfileHeader, "missing-profile")
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown bounty profile") {
		t.Fatalf("expected clear 400 for missing profile, got %d %s", rec.Code, rec.Body.String())
	}
	if hits != 0 {
		t.Fatalf("unknown profile request reached upstream %d time(s)", hits)
	}
}

// A synthetic request (warmup/naming) is answered locally and never reaches a
// provider. A stale or deleted profile id must still fail explicitly instead of
// appearing to be honoured on a request that carries no scope context.
func TestSyntheticRequestDoesNotSilentlyAcceptStaleProfile(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	h := NewChatHandler(db.NewRepo(database))

	// Warmup bodies are answered by the bypass path without any upstream call.
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"Warmup"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(BountyProfileHeader, "deleted-profile")
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown bounty profile") {
		t.Fatalf("synthetic request with stale profile should fail explicitly, got %d %s", rec.Code, rec.Body.String())
	}

	// Without a selector the same synthetic request is still served locally.
	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	plainRec := httptest.NewRecorder()
	h.HandleChatCompletions(plainRec, plain)
	if plainRec.Code != http.StatusOK {
		t.Fatalf("plain synthetic request should succeed, got %d %s", plainRec.Code, plainRec.Body.String())
	}
}

// ApplyPromptPlaneToBody must place the context in each protocol's real
// system field. OpenAI chat and Anthropic Messages are covered by
// TestApplyPromptPlaneToBody_PreservesCallerAndUsesProtocolSystemField.
// Responses must always use the top-level instructions field: `input` may be a
// string or an array, and role:"system" items inside input[] are invalid for
// the Responses API. A body that cannot carry instructions must error instead
// of silently forwarding without the operator-declared scope.
func TestApplyPromptPlaneToBody_ResponsesShapes(t *testing.T) {
	profile := bounty.Profile{ID: "p", Program: "Demo", InScope: []string{"api.demo.test"}}

	cases := []struct {
		name string
		body string
	}{
		{"input string, no instructions", `{"model":"m","input":"go"}`},
		{"input string, empty instructions", `{"model":"m","instructions":"","input":"go"}`},
		{"input string, null instructions", `{"model":"m","instructions":null,"input":"go"}`},
		{"input array of messages", `{"model":"m","input":[{"role":"user","content":"go"}]}`},
		{"input array of typed items", `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := ApplyPromptPlaneToBody([]byte(tc.body), PromptPlane{Bounty: &profile}, PromptWireResponses)
			if err != nil {
				t.Fatalf("expected injectable Responses body, got error: %v", err)
			}
			if !changed {
				t.Fatal("scope context was not injected")
			}
			var m map[string]any
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatal(err)
			}
			ins, ok := m["instructions"].(string)
			if !ok || !strings.Contains(ins, "api.demo.test") {
				t.Fatalf("Responses instructions did not receive scope: %s", out)
			}
			// input must be carried through untouched; no messages[] introduced.
			if _, ok := m["messages"]; ok {
				t.Fatalf("Responses body gained messages[]: %s", out)
			}
			if arr, ok := m["input"].([]any); ok {
				for i, item := range arr {
					if mm, ok := item.(map[string]any); ok && mm["role"] == "system" {
						t.Fatalf("role:system item inserted into Responses input[] at %d: %s", i, out)
					}
				}
			}
		})
	}

	t.Run("preserves caller instructions", func(t *testing.T) {
		out, _, err := ApplyPromptPlaneToBody([]byte(`{"model":"m","instructions":"Be brief.","input":"go"}`), PromptPlane{Bounty: &profile}, PromptWireResponses)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		ins := m["instructions"].(string)
		if !strings.Contains(ins, "Be brief.") || !strings.Contains(ins, "api.demo.test") {
			t.Fatalf("caller instructions lost or scope missing: %s", out)
		}
	})

	t.Run("idempotent on already-applied body", func(t *testing.T) {
		first, _, err := ApplyPromptPlaneToBody([]byte(`{"model":"m","input":"go"}`), PromptPlane{Bounty: &profile}, PromptWireResponses)
		if err != nil {
			t.Fatal(err)
		}
		_, changed, err := ApplyPromptPlaneToBody(first, PromptPlane{Bounty: &profile}, PromptWireResponses)
		if err != nil {
			t.Fatalf("re-applying must not error: %v", err)
		}
		if changed {
			t.Fatal("re-applying must be a no-op")
		}
	})

	t.Run("uninjectable bodies error", func(t *testing.T) {
		profile := bounty.Profile{ID: "p", Program: "Demo", InScope: []string{"api.demo.test"}}
		for _, bad := range []string{`{invalid`, `{"model":"m","instructions":42,"input":"go"}`, `not-json`} {
			if _, _, err := ApplyPromptPlaneToBody([]byte(bad), PromptPlane{Bounty: &profile}, PromptWireResponses); err == nil {
				t.Fatalf("expected error for uninjectable Responses body %q", bad)
			}
		}
	})
}
