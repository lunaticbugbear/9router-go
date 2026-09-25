package chat

import (
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
	"9router/proxy/internal/persona"
)

// seedPersonaPlane stores personas and the plane state for a test repository.
func seedPersonaPlane(t *testing.T, repo *db.Repo, enabled bool, defaultID string, personas ...persona.Persona) {
	t.Helper()
	for _, p := range personas {
		if err := repo.SetPersona(p); err != nil {
			t.Fatalf("seed persona %s: %v", p.ID, err)
		}
	}
	if err := repo.SetPersonasPlane(enabled, defaultID); err != nil {
		t.Fatalf("seed persona plane: %v", err)
	}
}

// Request header wins over the default, and with the plane off only an explicit
// header applies anything at all.
func TestPersonaResolutionPrecedence(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	seedPersonaPlane(t, repo, true, "default-one",
		persona.Persona{ID: "default-one", SystemPrompt: "default persona"},
		persona.Persona{ID: "explicit", SystemPrompt: "explicit persona"},
	)
	h := NewChatHandler(repo)

	tests := []struct {
		name     string
		header   string
		wantID   string
		wantNone bool
	}{
		{"no header uses the default while enabled", "", "default-one", false},
		{"header wins over the default", "explicit", "explicit", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tc.header != "" {
				r.Header.Set(PersonaHeader, tc.header)
			}
			got, err := h.PersonaForRequest(r)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if tc.wantNone {
				if got != nil {
					t.Fatalf("expected no persona, got %+v", got)
				}
				return
			}
			if got == nil || got.ID != tc.wantID {
				t.Fatalf("resolved %+v, want %q", got, tc.wantID)
			}
		})
	}

	// Plane disabled with no header: nothing is applied, even though a default
	// is configured. This is the both-settings-off default.
	if err := repo.SetPersonasPlane(false, "default-one"); err != nil {
		t.Fatal(err)
	}
	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	got, err := h.PersonaForRequest(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("a disabled plane applied a persona: %+v", got)
	}

	// An explicit header still resolves while the plane is disabled: a client
	// naming a persona gets it (or an error), never a silent no-op.
	explicit := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	explicit.Header.Set(PersonaHeader, "explicit")
	got, err = h.PersonaForRequest(explicit)
	if err != nil || got == nil || got.ID != "explicit" {
		t.Fatalf("explicit selection ignored while the plane is disabled: %+v err=%v", got, err)
	}
}

func TestPersonaResolutionFailsClosedOnUnknownOrDanglingSelector(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	seedPersonaPlane(t, repo, true, "gone", persona.Persona{ID: "gone", SystemPrompt: "x"})
	h := NewChatHandler(repo)

	unknown := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unknown.Header.Set(PersonaHeader, "not-configured")
	if _, err := h.PersonaForRequest(unknown); err == nil || !strings.Contains(err.Error(), "unknown persona") {
		t.Fatalf("unknown persona must fail clearly, got %v", err)
	}

	// Deleting the default leaves a dangling reference. Resolution must error
	// rather than render an empty persona into every request.
	if err := repo.DeletePersona("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PersonaForRequest(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)); err == nil {
		t.Fatal("a dangling default persona must fail closed")
	}

	// An empty default with the plane enabled is a valid state: nothing applies.
	if err := repo.SetPersonasPlane(true, ""); err != nil {
		t.Fatal(err)
	}
	got, err := h.PersonaForRequest(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	if err != nil || got != nil {
		t.Fatalf("empty default should apply nothing, got %+v err=%v", got, err)
	}
}

// A header naming a persona must not fall back to the configured default: a
// stale client is told, not served different instructions than it named.
func TestPersonaHeaderDoesNotFallBackToDefault(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	seedPersonaPlane(t, repo, true, "default-one", persona.Persona{ID: "default-one", SystemPrompt: "x"})
	h := NewChatHandler(repo)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set(PersonaHeader, "deleted-earlier")
	if _, err := h.PersonaForRequest(r); err == nil {
		t.Fatal("a stale header must error instead of silently using the default")
	}
}

// Both blocks must coexist in one body, each exactly once, and re-applying the
// plane must not duplicate either.
func TestApplyPromptPlaneToBody_PersonaAndBountyTogether(t *testing.T) {
	profile := bounty.Profile{ID: "h1", Program: "Demo program", InScope: []string{"api.demo.test"}}
	p := persona.Persona{ID: "terse", SystemPrompt: "Answer in one sentence.", AppendExisting: true}
	plane := PromptPlane{Persona: &p, Bounty: &profile}

	formats := []struct {
		name     string
		body     string
		format   PromptWireFormat
		systemAt func(t *testing.T, req map[string]any) string
	}{
		{
			name:   "openai chat",
			body:   `{"model":"m","messages":[{"role":"system","content":"caller rules"},{"role":"user","content":"go"}]}`,
			format: PromptWireOpenAIChat,
			systemAt: func(t *testing.T, req map[string]any) string {
				messages := req["messages"].([]any)
				if len(messages) != 2 {
					t.Fatalf("expected caller message preserved, got %d messages", len(messages))
				}
				return messages[0].(map[string]any)["content"].(string)
			},
		},
		{
			name:   "claude messages",
			body:   `{"model":"m","system":"caller rules","messages":[{"role":"user","content":"go"}]}`,
			format: PromptWireClaudeMessages,
			systemAt: func(t *testing.T, req map[string]any) string {
				if _, ok := req["system"].(string); !ok {
					t.Fatalf("Claude system must be a top-level string: %v", req["system"])
				}
				return req["system"].(string)
			},
		},
		{
			name:   "responses",
			body:   `{"model":"m","instructions":"caller rules","input":"go"}`,
			format: PromptWireResponses,
			systemAt: func(t *testing.T, req map[string]any) string {
				return req["instructions"].(string)
			},
		},
	}

	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := ApplyPromptPlaneToBody([]byte(tc.body), plane, tc.format)
			if err != nil {
				t.Fatalf("inject: %v", err)
			}
			if !changed {
				t.Fatal("expected both blocks to be added")
			}
			var req map[string]any
			if err := json.Unmarshal(out, &req); err != nil {
				t.Fatal(err)
			}
			system := tc.systemAt(t, req)
			if !strings.Contains(system, "caller rules") {
				t.Errorf("caller system content lost: %q", system)
			}
			if n := strings.Count(system, p.BuildSystemAddition()); n != 1 {
				t.Errorf("persona block appears %d times, want 1: %q", n, system)
			}
			if n := strings.Count(system, profile.BuildSystemContext()); n != 1 {
				t.Errorf("bounty block appears %d times, want 1: %q", n, system)
			}
			// Merge order is documented: persona first, then the scope block.
			if strings.Index(system, p.BuildSystemAddition()) > strings.Index(system, profile.BuildSystemContext()) {
				t.Errorf("persona must precede the bounty scope block: %q", system)
			}

			// Re-applying the same plane must be a complete no-op.
			again, changedAgain, err := ApplyPromptPlaneToBody(out, plane, tc.format)
			if err != nil {
				t.Fatalf("re-apply: %v", err)
			}
			if changedAgain {
				t.Fatal("re-applying the same plane must not change the body")
			}
			if string(again) != string(out) {
				t.Fatalf("re-applying rewrote the body:\n%s\n%s", out, again)
			}
		})
	}
}

// A replacement persona drops the caller's own system text (that is the explicit
// operator choice), while an appended persona keeps it.
func TestApplyPromptPlaneToBody_ReplacementDropsCallerSystemOnly(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"system","content":"caller rules"},{"role":"user","content":"go"}]}`)

	appended := persona.Persona{ID: "p", SystemPrompt: "persona text", AppendExisting: true}
	out, _, err := ApplyPromptPlaneToBody(body, PromptPlane{Persona: &appended}, PromptWireOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	system := firstSystemContent(t, out)
	if !strings.Contains(system, "caller rules") || !strings.Contains(system, "persona text") {
		t.Fatalf("append mode must keep both: %q", system)
	}

	replacing := persona.Persona{ID: "p", SystemPrompt: "persona text"}
	out, _, err = ApplyPromptPlaneToBody(body, PromptPlane{Persona: &replacing}, PromptWireOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	system = firstSystemContent(t, out)
	if strings.Contains(system, "caller rules") {
		t.Fatalf("replacement mode must drop caller system text: %q", system)
	}
	if !strings.Contains(system, "persona text") {
		t.Fatalf("replacement mode lost the persona: %q", system)
	}

	// In replacement mode the bounty block is applied afterwards and therefore
	// survives, preceded only by the persona.
	withBounty := PromptPlane{Persona: &replacing, Bounty: &bounty.Profile{ID: "h1", Program: "Demo", InScope: []string{"api.demo.test"}}}
	out, _, err = ApplyPromptPlaneToBody(body, withBounty, PromptWireOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	system = firstSystemContent(t, out)
	if strings.Contains(system, "caller rules") || !strings.Contains(system, "api.demo.test") {
		t.Fatalf("scope block must survive a replacement persona: %q", system)
	}
}

func firstSystemContent(t *testing.T, body []byte) string {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	for _, m := range req["messages"].([]any) {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "system" {
			content, _ := msg["content"].(string)
			return content
		}
	}
	t.Fatalf("no system message in %s", body)
	return ""
}

// Either uninjectable piece must fail the whole application: a request that
// named a persona must never be forwarded without it.
func TestApplyPromptPlaneToBody_UninjectablePieceFailsClosed(t *testing.T) {
	p := persona.Persona{ID: "p", SystemPrompt: "persona text"}
	profile := bounty.Profile{ID: "h1", Program: "Demo", InScope: []string{"api.demo.test"}}
	// A Responses-shaped body is uninjectable for chat, which reads messages[].
	badBody := []byte(`{"model":"m","input":[{"role":"user","content":"go"}]}`)

	for _, spec := range []PromptPlane{
		{Persona: &p},
		{Bounty: &profile},
		{Persona: &p, Bounty: &profile},
	} {
		if _, _, err := ApplyPromptPlaneToBody(badBody, spec, PromptWireOpenAIChat); err == nil {
			t.Fatalf("expected an error for spec %+v on an uninjectable body", spec)
		}
	}
}

// attachPromptPlane must resolve both selectors in one pass and carry both.
func TestAttachPromptPlaneCarriesBothSelectors(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetBountyProfile(bounty.Profile{ID: "h1", Program: "Demo", InScope: []string{"a.example"}}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set(PersonaHeader, "terse")
	r.Header.Set(BountyProfileHeader, "h1")
	ctx, err := h.attachPromptPlane(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	plane, ok := promptPlaneFromContext(ctx)
	if !ok || plane.Persona == nil || plane.Bounty == nil {
		t.Fatalf("both selectors must be carried: %+v ok=%v", plane, ok)
	}
	if plane.Persona.ID != "terse" || plane.Bounty.ID != "h1" {
		t.Fatalf("wrong ids carried: %+v", plane)
	}

	// Header strip removes both selectors, whichever static header re-adds them.
	headers := http.Header{}
	headers.Set(PersonaHeader, "terse")
	headers.Set(BountyProfileHeader, "h1")
	StripInternalSelectorHeaders(headers)
	if headers.Get(PersonaHeader) != "" || headers.Get(BountyProfileHeader) != "" {
		t.Fatalf("selector headers survived the strip: %v", headers)
	}
}
