package chat

import (
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
	"9router/proxy/internal/persona"
)

// A stored persona selected by header must reach the outgoing body in the
// protocol's system field and must not appear as a forwarded header.
func TestHandleChatCompletions_PersonaAppliedAndSelectorStripped(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	var receivedPersonaHeader, receivedBountyHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedPersonaHeader = r.Header.Get(PersonaHeader)
		receivedBountyHeader = r.Header.Get(BountyProfileHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-persona", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{
		ID: "terse", SystemPrompt: "Answer in one sentence.", AppendExisting: true,
	}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"system","content":"caller rules"},{"role":"user","content":"explain"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set(PersonaHeader, "terse")
	w := httptest.NewRecorder()
	h.HandleChatCompletions(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("chat failed: %d %s", w.Code, w.Body.String())
	}

	if receivedPersonaHeader != "" || receivedBountyHeader != "" {
		t.Fatalf("selector headers leaked upstream: persona=%q bounty=%q", receivedPersonaHeader, receivedBountyHeader)
	}
	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	messages := sent["messages"].([]any)
	system := messages[0].(map[string]any)["content"].(string)
	for _, want := range []string{"caller rules", "Answer in one sentence.", "operator-declared", "provider safety policies remain authoritative"} {
		if !strings.Contains(system, want) {
			t.Errorf("outgoing system content missing %q: %q", want, system)
		}
	}
	if got := messages[1].(map[string]any)["content"]; got != "explain" {
		t.Errorf("user message was modified: %#v", got)
	}
}

// Persona and bounty selectors together must produce exactly one of each block.
func TestHandleChatCompletions_PersonaAndBountyBothAppliedOnce(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-both", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text", AppendExisting: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetBountyProfile(bounty.Profile{ID: "h1", Program: "Demo program", InScope: []string{"api.demo.test"}}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"analyze"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set(PersonaHeader, "terse")
	r.Header.Set(BountyProfileHeader, "h1")
	w := httptest.NewRecorder()
	h.HandleChatCompletions(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("chat failed: %d %s", w.Code, w.Body.String())
	}

	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	messages := sent["messages"].([]any)
	system := messages[0].(map[string]any)["content"].(string)
	if n := strings.Count(system, "persona-only-text"); n != 1 {
		t.Errorf("persona block appears %d times, want 1: %q", n, system)
	}
	if n := strings.Count(system, "api.demo.test"); n != 1 {
		t.Errorf("bounty scope appears %d times, want 1: %q", n, system)
	}
	if strings.Index(system, "persona-only-text") > strings.Index(system, "api.demo.test") {
		t.Errorf("persona must precede the bounty scope block: %q", system)
	}
}

// Without a selector and with the plane off, the body must reach the provider
// byte-for-byte as sent.
func TestHandleChatCompletions_NoSelectorLeavesBodyUntouched(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-plain", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	// A persona exists but the plane is off and nothing names it.
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text"}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleChatCompletions(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("chat failed: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(string(receivedBody), "persona-only-text") {
		t.Fatalf("an unselected persona was injected: %s", receivedBody)
	}
	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	messages := sent["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("unselected request gained system content: %s", receivedBody)
	}
}

// A stored default persona applies when the plane is enabled, and stops the
// moment it is disabled.
func TestHandleChatCompletions_DefaultPersonaHonorsPlaneSwitch(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var bodies [][]byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-default", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "house-style", SystemPrompt: "house-style-text", AppendExisting: true}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`
	h := NewChatHandler(repo)

	send := func() {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.HandleChatCompletions(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("chat failed: %d %s", w.Code, w.Body.String())
		}
	}

	// Plane off (the default): no persona.
	send()
	if strings.Contains(string(bodies[0]), "house-style-text") {
		t.Fatalf("disabled plane applied the default persona: %s", bodies[0])
	}

	if err := repo.SetPersonasPlane(true, "house-style"); err != nil {
		t.Fatal(err)
	}
	send()
	if !strings.Contains(string(bodies[1]), "house-style-text") {
		t.Fatalf("enabled plane did not apply the default persona: %s", bodies[1])
	}

	if err := repo.SetPersonasPlane(false, "house-style"); err != nil {
		t.Fatal(err)
	}
	send()
	if strings.Contains(string(bodies[2]), "house-style-text") {
		t.Fatalf("disabling the plane did not stop the default persona: %s", bodies[2])
	}
}

func TestUnknownPersonaRejectsBeforeUpstream(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"unexpected"}}]}`))
	}))
	defer upstream.Close()
	seedConnDB(t, database, "deepseek", "conn-unknown-persona", "sk-test", upstream.URL)
	h := NewChatHandler(db.NewRepo(database))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set(PersonaHeader, "missing-persona")
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown persona") {
		t.Fatalf("expected clear 400 for missing persona, got %d %s", rec.Code, rec.Body.String())
	}
	if hits != 0 {
		t.Fatalf("unknown persona request reached upstream %d time(s)", hits)
	}
}

// A selected persona whose body cannot carry it must be refused before any
// upstream call rather than forwarded without the instructions.
func TestPersonaUninjectableBodyRejectedBeforeUpstream(t *testing.T) {
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

			seedConnDB(t, database, "deepseek", "conn-uninj-persona", "sk-test", upstream.URL)
			repo := db.NewRepo(database)
			if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "x"}); err != nil {
				t.Fatal(err)
			}

			h := NewChatHandler(repo)
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			r.Header.Set(PersonaHeader, "terse")
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

// The persona is not sticky: a later request without the header must not inherit
// the previous selection.
func TestPersonaIsNotStickyAcrossRequests(t *testing.T) {
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
	seedConnDB(t, database, "deepseek", "conn-sticky", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text"}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"review"}]}`

	selected := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	selected.Header.Set(PersonaHeader, "terse")
	selectedRec := httptest.NewRecorder()
	h.HandleChatCompletions(selectedRec, selected)
	if selectedRec.Code != http.StatusOK {
		t.Fatalf("selected request failed: %d %s", selectedRec.Code, selectedRec.Body.String())
	}

	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	plainRec := httptest.NewRecorder()
	h.HandleChatCompletions(plainRec, plain)
	if plainRec.Code != http.StatusOK {
		t.Fatalf("plain request failed: %d %s", plainRec.Code, plainRec.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(bodies))
	}
	if !strings.Contains(string(bodies[0]), "persona-only-text") {
		t.Fatalf("selected request did not receive the persona: %s", bodies[0])
	}
	if strings.Contains(string(bodies[1]), "persona-only-text") {
		t.Fatalf("persona stuck to a later unselected request: %s", bodies[1])
	}
}

// The Anthropic /v1/messages path must resolve the same selectors as
// /v1/chat/completions. Both handlers end in the same fallback, which is the
// only reader of the resolved plane, so a request that carries a persona here
// must reach upstream with the persona applied — and with the selector header
// stripped, exactly as on the chat-completions path.
//
// This is the regression test for the plane being attached on only one of the
// two paths: the header was accepted, no error was raised, no log line was
// written, and the outbound body simply had no persona in it.
func TestHandleMessages_PersonaAppliedAndSelectorStripped(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	var receivedPersonaHeader, receivedBountyHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedPersonaHeader = r.Header.Get(PersonaHeader)
		receivedBountyHeader = r.Header.Get(BountyProfileHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-msg-persona", "sk-test", upstream.URL)
	// Drop the pre-seeded connections so the mock is the only reachable upstream:
	// a leftover real-provider connection would be tried first and this test must
	// not depend on network access.
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE id <> 'conn-msg-persona'`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{
		ID: "terse", SystemPrompt: "Answer in one sentence.", AppendExisting: true,
	}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"deepseek/deepseek-chat","system":"caller rules","messages":[{"role":"user","content":"explain"}],"max_tokens":100}`
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	r.Header.Set(PersonaHeader, "terse")
	w := httptest.NewRecorder()
	h.HandleMessages(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("messages failed: %d %s", w.Code, w.Body.String())
	}
	if receivedPersonaHeader != "" || receivedBountyHeader != "" {
		t.Fatalf("selector headers leaked upstream: persona=%q bounty=%q", receivedPersonaHeader, receivedBountyHeader)
	}
	if !strings.Contains(string(receivedBody), "Answer in one sentence.") {
		t.Fatalf("/v1/messages dropped the selected persona: %s", receivedBody)
	}

	// The persona must land in the translated OpenAI system message, beside the
	// caller's own system text, and the user turn must be untouched.
	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	messages, ok := sent["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("translated body has no messages: %s", receivedBody)
	}
	system := messages[0].(map[string]any)["content"].(string)
	for _, want := range []string{"caller rules", "Answer in one sentence.", "operator-declared", "provider safety policies remain authoritative"} {
		if !strings.Contains(system, want) {
			t.Errorf("outgoing system content missing %q: %q", want, system)
		}
	}
	if got := messages[len(messages)-1].(map[string]any)["content"]; got != "explain" {
		t.Errorf("user message was modified: %#v", got)
	}
}

// Parity with the chat-completions path: an unknown persona id on /v1/messages
// is a client mistake and must be reported before any upstream call, not
// silently served without the instructions the client asked for.
func TestHandleMessages_UnknownPersonaRejectedBeforeUpstream(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"unexpected"}}]}`))
	}))
	defer upstream.Close()
	seedConnDB(t, database, "deepseek", "conn-msg-unknown-persona", "sk-test", upstream.URL)
	// Only the mock is reachable, so a request that got past the selector check
	// would be observed here rather than on a real provider.
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE id <> 'conn-msg-unknown-persona'`); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(db.NewRepo(database))
	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"x"}],"max_tokens":10}`
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	r.Header.Set(PersonaHeader, "missing-persona")
	w := httptest.NewRecorder()
	h.HandleMessages(w, r)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown persona") {
		t.Fatalf("expected a clear 400 for a missing persona, got %d %s", w.Code, w.Body.String())
	}
	if hits != 0 {
		t.Fatalf("unknown persona request reached upstream %d time(s)", hits)
	}
}

// The Claude-native branch of the persona plane: a /v1/messages request to a
// genuinely Anthropic-headed upstream. Here the body stays in Claude Messages
// format, so the persona must be spliced into the top-level "system" field by
// PromptWireClaudeMessages — not into an OpenAI messages[] array.
//
// The existing /v1/messages test seeds a deepseek connection, so claudeNative is
// false there and the Claude branch of fallback.go was never exercised: it
// covered the translated path only. This pins the other half.
//
// The connection is made Anthropic-headed the way the code elsewhere does it: a
// vercel edge relay pool rewrites the config to the mock server's URL while
// carrying x-relay-target/x-relay-path for api.anthropic.com/v1/messages, which
// is exactly the shape isAnthropicUpstream matches on.
func TestHandleMessages_PersonaAppliedOnClaudeNativePath(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()

	var receivedBody []byte
	var receivedPersonaHeader, receivedBountyHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedPersonaHeader = r.Header.Get(PersonaHeader)
		receivedBountyHeader = r.Header.Get(BountyProfileHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-sonnet-4-6","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	// The mock must be the relay target, so the client reaches it directly.
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}
	repo := db.NewRepo(database)
	pool, err := repo.InsertProxyPool(db.ProxyPoolData{
		Name:     "anthropic-relay",
		ProxyURL: upstream.URL,
		Type:     "vercel",
	})
	if err != nil {
		t.Fatalf("failed to insert proxy pool: %v", err)
	}
	poolID, _ := pool["id"].(string)
	if poolID == "" {
		t.Fatal("proxy pool insert returned no id")
	}

	seedConnDB(t, database, "claude", "conn-native", "sk-ant-test", "https://api.anthropic.com/v1/messages")
	if _, err := database.Exec(`UPDATE providerConnections SET data = ? WHERE id = 'conn-native'`,
		`{"apiKey":"sk-ant-test","proxyPoolId":"`+poolID+`"}`); err != nil {
		t.Fatal(err)
	}
	// Only the mock is reachable; a leftover real-provider connection would be
	// tried first and this test must not depend on network access.
	if _, err := database.Exec(`DELETE FROM providerConnections WHERE id <> 'conn-native'`); err != nil {
		t.Fatal(err)
	}

	repo = db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{
		ID: "terse", SystemPrompt: "Answer in one sentence.", AppendExisting: true,
	}); err != nil {
		t.Fatal(err)
	}

	h := NewChatHandler(repo)
	body := `{"model":"claude/claude-sonnet-4-6","system":"caller rules","messages":[{"role":"user","content":"explain"}],"max_tokens":100}`
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	r.Header.Set(PersonaHeader, "terse")
	w := httptest.NewRecorder()
	h.HandleMessages(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("messages failed: %d %s", w.Code, w.Body.String())
	}
	if len(receivedBody) == 0 {
		t.Fatal("no upstream request reached the mock relay")
	}
	if receivedPersonaHeader != "" || receivedBountyHeader != "" {
		t.Fatalf("selector headers leaked upstream: persona=%q bounty=%q", receivedPersonaHeader, receivedBountyHeader)
	}

	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	// Claude-native: the persona belongs in the top-level system field, and the
	// body must still be Claude-shaped (no OpenAI messages[] translation).
	system, ok := sent["system"].(string)
	if !ok {
		t.Fatalf("Claude-native body must carry a top-level string system field, got %#v: %s", sent["system"], receivedBody)
	}
	for _, want := range []string{"caller rules", "Answer in one sentence.", "operator-declared", "provider safety policies remain authoritative"} {
		if !strings.Contains(system, want) {
			t.Errorf("outgoing system field missing %q: %q", want, system)
		}
	}
	if _, translated := sent["messages"].([]any); !translated {
		t.Fatalf("Claude-native body lost its messages[] array: %s", receivedBody)
	}
	if sent["max_tokens"] == nil {
		t.Errorf("Claude-native body lost max_tokens: %s", receivedBody)
	}
}
