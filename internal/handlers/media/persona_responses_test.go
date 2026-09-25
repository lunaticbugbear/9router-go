package media

import (
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/chat"
	"9router/proxy/internal/persona"
)

// /responses must carry the persona in the top-level instructions field and
// must never forward the internal selectors.
func TestHandleResponses_PersonaAppliedAndSelectorsStripped(t *testing.T) {
	database, cleanup := setupResponsesTestDB(t)
	defer cleanup()

	var receivedBody []byte
	var gotPersonaHeader, gotBountyHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		gotPersonaHeader = r.Header.Get(chat.PersonaHeader)
		gotBountyHeader = r.Header.Get(chat.BountyProfileHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp-1","output":[{"type":"text","text":"ok"}]}`))
	}))
	defer upstream.Close()

	connData, _ := json.Marshal(map[string]any{"apiKey": "sk-test-key", "baseUrl": upstream.URL})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-persona', 'deepseek', 'apikey', 'Test', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		string(connData)); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text", AppendExisting: true}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetBountyProfile(bounty.Profile{ID: "h1", Program: "Demo", InScope: []string{"api.demo.test"}}); err != nil {
		t.Fatal(err)
	}
	handler := newTestMediaHandler(repo)

	body := `{"model":"deepseek/deepseek-chat","instructions":"caller rules","input":"go"}`
	req := httptest.NewRequest("POST", "/responses", strings.NewReader(body))
	req.Header.Set(chat.PersonaHeader, "terse")
	req.Header.Set(chat.BountyProfileHeader, "h1")
	rec := httptest.NewRecorder()
	handler.HandleResponses(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if gotPersonaHeader != "" || gotBountyHeader != "" {
		t.Fatalf("selector headers leaked upstream: persona=%q bounty=%q", gotPersonaHeader, gotBountyHeader)
	}
	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	instructions, ok := sent["instructions"].(string)
	if !ok {
		t.Fatalf("instructions missing or not a string: %v", sent["instructions"])
	}
	for _, want := range []string{"caller rules", "persona-only-text", "api.demo.test"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions missing %q: %q", want, instructions)
		}
	}
	if n := strings.Count(instructions, "persona-only-text"); n != 1 {
		t.Errorf("persona block appears %d times: %q", n, instructions)
	}
	if n := strings.Count(instructions, "api.demo.test"); n != 1 {
		t.Errorf("scope block appears %d times: %q", n, instructions)
	}
	if _, ok := sent["messages"]; ok {
		t.Fatalf("Responses body gained messages[]: %s", receivedBody)
	}
}

// An unknown persona on /responses must be refused before any upstream call, and
// a body that cannot carry instructions — including form-encoded input — must
// fail closed rather than silently forward without the persona.
func TestHandleResponses_SelectorFailClosed(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		body        string
		contentType string
		wantMessage string
	}{
		{
			name:        "unknown persona id",
			header:      "missing-persona",
			body:        `{"model":"deepseek/deepseek-chat","input":"go"}`,
			contentType: "application/json",
			wantMessage: "unknown persona",
		},
		{
			name:        "unknown bounty profile id",
			header:      "missing-bounty",
			body:        `{"model":"deepseek/deepseek-chat","input":"go"}`,
			contentType: "application/json",
			wantMessage: "unknown bounty profile",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			database, cleanup := setupResponsesTestDB(t)
			defer cleanup()

			var calls int
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"resp","output":[]}`))
			}))
			defer upstream.Close()

			connData, _ := json.Marshal(map[string]any{"apiKey": "sk-test-key", "baseUrl": upstream.URL})
			if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
				('conn-fc', 'deepseek', 'apikey', 'Test', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
				string(connData)); err != nil {
				t.Fatalf("insert connection: %v", err)
			}

			handler := newTestMediaHandler(db.NewRepo(database))
			req := httptest.NewRequest("POST", "/responses", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if strings.Contains(tc.header, "persona") {
				req.Header.Set(chat.PersonaHeader, tc.header)
			} else {
				req.Header.Set(chat.BountyProfileHeader, tc.header)
			}
			rec := httptest.NewRecorder()
			handler.HandleResponses(rec, req)

			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.wantMessage) {
				t.Fatalf("expected 400 mentioning %q, got %d: %s", tc.wantMessage, rec.Code, rec.Body.String())
			}
			if calls != 0 {
				t.Fatalf("rejected request reached upstream %d time(s)", calls)
			}
		})
	}
}

// A form-encoded /responses body is not JSON and therefore cannot carry
// instructions; a selected persona must fail closed instead of being dropped.
func TestHandleResponses_NonJSONBodyWithSelectorFailsClosed(t *testing.T) {
	database, cleanup := setupResponsesTestDB(t)
	defer cleanup()

	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp","output":[]}`))
	}))
	defer upstream.Close()

	connData, _ := json.Marshal(map[string]any{"apiKey": "sk-test-key", "baseUrl": upstream.URL})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-form', 'deepseek', 'apikey', 'Test', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		string(connData)); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text"}); err != nil {
		t.Fatal(err)
	}
	handler := newTestMediaHandler(repo)

	req := httptest.NewRequest("POST", "/responses", strings.NewReader("model=x&input=go"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(chat.PersonaHeader, "terse")
	rec := httptest.NewRecorder()
	handler.HandleResponses(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a body that cannot carry the persona, got %d: %s", rec.Code, rec.Body.String())
	}
	if calls != 0 {
		t.Fatalf("uninjectable request reached upstream %d time(s)", calls)
	}
}

// Without a selector the body must be forwarded untouched.
func TestHandleResponses_NoSelectorLeavesBodyUntouched(t *testing.T) {
	database, cleanup := setupResponsesTestDB(t)
	defer cleanup()

	var receivedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp","output":[]}`))
	}))
	defer upstream.Close()

	connData, _ := json.Marshal(map[string]any{"apiKey": "sk-test-key", "baseUrl": upstream.URL})
	if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt) VALUES
		('conn-plain', 'deepseek', 'apikey', 'Test', 0, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
		string(connData)); err != nil {
		t.Fatalf("insert connection: %v", err)
	}

	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "persona-only-text"}); err != nil {
		t.Fatal(err)
	}
	handler := newTestMediaHandler(repo)

	body := `{"model":"deepseek/deepseek-chat","input":"go"}`
	req := httptest.NewRequest("POST", "/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.HandleResponses(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var sent map[string]any
	if err := json.Unmarshal(receivedBody, &sent); err != nil {
		t.Fatal(err)
	}
	if _, ok := sent["instructions"]; ok {
		t.Fatalf("unselected request gained instructions: %s", receivedBody)
	}
	if strings.Contains(string(receivedBody), "persona-only-text") {
		t.Fatalf("an unselected persona was injected: %s", receivedBody)
	}
}
