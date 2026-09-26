package chat

import (
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
	"9router/proxy/internal/featureflags"
	"9router/proxy/internal/handlers/shared"
	"9router/proxy/internal/modelalias"
	"9router/proxy/internal/persona"
)

// The flag constants are the contract between this package and the registry. A
// rename in the registry would otherwise turn a gate into "unknown flag", which
// fails closed and is indistinguishable from an operator switching the feature
// off — so the constants are pinned against the registry here.
func TestFlagIDsAreRegistered(t *testing.T) {
	for _, id := range []string{flagPersonaPlane, flagModelBindings, flagRTKSaver} {
		def, ok := featureflags.Get(id)
		if !ok {
			t.Errorf("gate flag %q is not in the registry", id)
			continue
		}
		if def.Stage != featureflags.Stable {
			t.Errorf("%q is gated but is not a stable flag (stage=%s)", id, def.Stage)
		}
	}
}

// The helper is the single read path for gating, so its fallback behavior is
// worth pinning: a storage failure must fall back to the registry default, not
// to "off", or an unrelated DB problem would silently disable a stable feature.
func TestFeatureFlagOnFallsBackToRegistryDefault(t *testing.T) {
	// No repo at all: several handlers are constructed without one.
	bare := NewChatHandler(nil)
	if !bare.featureFlagOn(flagPersonaPlane) {
		t.Error("nil repo must fall back to the registry default (on), not off")
	}
	if bare.featureFlagOn("no.such.flag") {
		t.Error("an unknown flag id must fail closed")
	}

	// With storage: stored choices win, in both directions.
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	h := NewChatHandler(repo)

	if !h.featureFlagOn(flagPersonaPlane) {
		t.Error("an untouched default-on flag must report on")
	}
	if err := repo.SetFeatureFlag(flagPersonaPlane, false); err != nil {
		t.Fatal(err)
	}
	if h.featureFlagOn(flagPersonaPlane) {
		t.Error("a stored off choice must win over the registry default")
	}
	if err := repo.SetFeatureFlag(flagRTKSaver, false); err != nil {
		t.Fatal(err)
	}
	// Two flags with the same stored value must not be conflated by a shared
	// cache key; flip one back and confirm they diverge.
	if err := repo.SetFeatureFlag(flagPersonaPlane, true); err != nil {
		t.Fatal(err)
	}
	if !h.featureFlagOn(flagPersonaPlane) || h.featureFlagOn(flagRTKSaver) {
		t.Error("flags must resolve independently of each other")
	}
}

// With prompt.personas off the persona plane must be inert: no header, no
// configured default, and no binding may select a persona. An explicit header is
// deliberately NOT an error here — the feature is intentionally off, so erroring
// would turn the operator's choice into a client-visible outage.
func TestPersonaPlaneOffIgnoresEverySelector(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)

	// Everything that could select a persona is configured and on, so the flag is
	// the only reason nothing is selected.
	seedPersonaPlane(t, repo, true, "default-one",
		persona.Persona{ID: "default-one", SystemPrompt: "default persona"},
		persona.Persona{ID: "explicit", SystemPrompt: "explicit persona"},
	)
	if err := repo.SetFeatureFlag(flagPersonaPlane, false); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	cases := []struct {
		name   string
		header string
	}{
		{"explicit header ignored without error", "explicit"},
		{"header naming an unknown persona ignored without error", "never-stored"},
		{"header naming a deleted persona ignored without error", "deleted-earlier"},
		{"no header applies no default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tc.header != "" {
				r.Header.Set(PersonaHeader, tc.header)
			}
			got, err := h.PersonaForRequest(r)
			if err != nil {
				t.Fatalf("the flag being off must not error, got %v", err)
			}
			if got != nil {
				t.Fatalf("flag off must resolve no persona, got %+v", got)
			}
		})
	}

	// And the whole plane resolves empty, so nothing is attached downstream.
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set(PersonaHeader, "explicit")
	ctx, err := h.attachPromptPlane(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	if plane, ok := promptPlaneFromContext(ctx); ok {
		t.Fatalf("no plane must be attached while the flag is off, got %+v", plane)
	}

	// Turning the flag back on restores the pre-flag behavior, including the
	// fail-closed rule for an unknown persona.
	if err := repo.SetFeatureFlag(flagPersonaPlane, true); err != nil {
		t.Fatal(err)
	}
	on := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	on.Header.Set(PersonaHeader, "explicit")
	got, err := h.PersonaForRequest(on)
	if err != nil || got == nil || got.ID != "explicit" {
		t.Fatalf("flag on must resolve the header again: %+v err=%v", got, err)
	}
	unknown := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	unknown.Header.Set(PersonaHeader, "never-stored")
	if _, err := h.PersonaForRequest(unknown); err == nil || !strings.Contains(err.Error(), "unknown persona") {
		t.Fatalf("flag on must keep the fail-closed rule, got %v", err)
	}
}

// The bounty selector is a different feature and must keep working while the
// persona plane is off: gating one must not disable the other.
func TestPersonaPlaneOffLeavesBountyUntouched(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetFeatureFlag(flagPersonaPlane, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetBountyProfile(bounty.Profile{
		ID: "h1-flags", Program: "Demo", InScope: []string{"api.demo.test"},
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set(BountyProfileHeader, "h1-flags")
	plane, err := h.PromptPlaneForRequest(r, "")
	if err != nil {
		t.Fatal(err)
	}
	if plane.Persona != nil {
		t.Fatalf("persona plane must stay inert: %+v", plane.Persona)
	}
	if plane.Bounty == nil || plane.Bounty.ID != "h1-flags" {
		t.Fatalf("bounty resolution must be unaffected by the persona flag: %+v", plane.Bounty)
	}
}

// A binding's persona is part of the binding, so with prompt.bindings off the
// persona must not be resolved even while the persona plane itself is on.
func TestBindingPersonaNotResolvedWhileBindingsOff(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetPersona(persona.Persona{ID: "bound", SystemPrompt: "bound persona"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetModelBinding(modelalias.Binding{
		ID: "ltx-mod", Target: "deepseek/deepseek-chat", Persona: "bound", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Persona plane stays on, so only the bindings flag can explain the result.
	h := NewChatHandler(repo)

	got, err := h.personaFromModelBinding("ltx-mod")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != "bound" {
		t.Fatalf("bindings on must resolve the bound persona: %+v", got)
	}

	if err := repo.SetFeatureFlag(flagModelBindings, false); err != nil {
		t.Fatal(err)
	}
	got, err = h.personaFromModelBinding("ltx-mod")
	if err != nil {
		t.Fatalf("bindings off must not error, got %v", err)
	}
	if got != nil {
		t.Fatalf("bindings off must resolve no persona, got %+v", got)
	}

	// With bindings on but the persona plane off, the persona is still withheld:
	// otherwise a binding would keep splicing a persona into outbound bodies
	// while the operator had switched the plane off.
	if err := repo.SetFeatureFlag(flagModelBindings, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetFeatureFlag(flagPersonaPlane, false); err != nil {
		t.Fatal(err)
	}
	got, err = h.personaFromModelBinding("ltx-mod")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("persona plane off must withhold a binding's persona, got %+v", got)
	}
}

// With prompt.bindings off a bound name must resolve as if no binding existed:
// the name is no longer rewritten to the binding's target. What it resolves to
// instead depends on the rest of resolution, and both outcomes are asserted here
// so the gate is pinned to "no rewrite" rather than to one downstream accident.
func TestBindingRewriteSkippedWhileBindingsOff(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetModelBinding(modelalias.Binding{
		ID: "ltx-mod", Target: "deepseek/deepseek-chat", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	info, err := h.resolveModel("ltx-mod")
	if err != nil {
		t.Fatalf("bindings on must resolve the bound name: %v", err)
	}
	if info.Model != "deepseek-chat" {
		t.Fatalf("expected the binding target, got %+v", info)
	}

	// Bindings off. The shared test DB has a deepseek connection, so the
	// pre-existing common-provider fallback still catches the bare name — the
	// gate removes the rewrite, it does not disable resolution. The model name
	// must therefore survive unrewritten.
	if err := repo.SetFeatureFlag(flagModelBindings, false); err != nil {
		t.Fatal(err)
	}
	info, err = h.resolveModel("ltx-mod")
	if err != nil {
		t.Fatalf("bindings off must not turn a resolvable name into an error: %v", err)
	}
	if info.Model != "ltx-mod" {
		t.Fatalf("bindings off must not rewrite the name, got %+v", info)
	}
	if info.Model == "deepseek-chat" {
		t.Fatal("bindings off still applied the binding target")
	}
}

// The same gate, in the state the flag's description promises: when nothing else
// can serve the bound name, turning bindings off leaves it an unknown model
// rather than a silent rewrite to the target.
func TestBindingRewriteOffLeavesUnknownModelWhenNothingElseServesIt(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	// No connections at all, so the common-provider fallback cannot catch the
	// bare name and resolution has nothing left to try.
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatal(err)
	}
	repo := db.NewRepo(database)
	if err := repo.SetModelBinding(modelalias.Binding{
		ID: "ltx-mod", Target: "deepseek/deepseek-chat", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	if err := repo.SetFeatureFlag(flagModelBindings, false); err != nil {
		t.Fatal(err)
	}
	info, err := h.resolveModel("ltx-mod")
	if err == nil {
		t.Fatalf("bindings off must leave the name unresolved, got %+v", info)
	}
	if !strings.Contains(err.Error(), "could not resolve model") {
		t.Fatalf("expected an unknown-model error, got %v", err)
	}

	// Turning the flag back on restores the rewrite.
	if err := repo.SetFeatureFlag(flagModelBindings, true); err != nil {
		t.Fatal(err)
	}
	info, err = h.resolveModel("ltx-mod")
	if err != nil {
		t.Fatalf("bindings on must resolve again: %v", err)
	}
	if info.Model != "deepseek-chat" {
		t.Fatalf("expected the binding target once the flag is back on, got %+v", info)
	}
}

// The gate must not fire while the flag is on, including for a binding that is
// itself disabled: that is modelalias's own rule, not the feature flag's. A
// disabled binding has no target, so resolution falls through to the normal
// paths exactly as it would for an unbound name.
func TestBindingDisabledStillResolvesNothingWithFlagOn(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	repo := db.NewRepo(database)
	if err := repo.SetModelBinding(modelalias.Binding{
		ID: "off-mod", Target: "deepseek/deepseek-chat", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	info, err := h.resolveModel("off-mod")
	if err != nil {
		t.Fatalf("a disabled binding must not error: %v", err)
	}
	if info.Model != "off-mod" {
		t.Fatalf("a disabled binding must not rewrite the name, got %+v", info)
	}
}

// The off-state must be visible end to end: with bindings off, the name that
// reaches the provider is the one the client sent, not the binding's target.
func TestHandleChatCompletions_BindingsOffSendsUnrewrittenName(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	// Drop the shared DB's connections so this test's mock upstream is the only
	// reachable one; otherwise the seeded deepseek connection wins and the
	// request leaves for the real api.deepseek.com.
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatal(err)
	}

	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-bind-off", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	if err := repo.SetModelBinding(modelalias.Binding{
		ID: "ltx-mod", Target: "deepseek/deepseek-chat", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	send := func() map[string]any {
		t.Helper()
		body := `{"model":"ltx-mod","messages":[{"role":"user","content":"hi"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request failed: %d %s", rec.Code, rec.Body.String())
		}
		var sent map[string]any
		if err := json.Unmarshal(received, &sent); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		return sent
	}

	// Flag on: the binding rewrites the name on the wire.
	if sent := send(); sent["model"] != "deepseek-chat" {
		t.Fatalf("bindings on should rewrite the outbound model, got %v", sent["model"])
	}

	// Flag off: the client's own name is forwarded untouched.
	if err := repo.SetFeatureFlag(flagModelBindings, false); err != nil {
		t.Fatal(err)
	}
	if sent := send(); sent["model"] != "ltx-mod" {
		t.Fatalf("bindings off must forward the client's model name, got %v", sent["model"])
	}
}

// A body that names a bound model and carries an explicit persona header must
// come out of the forward path with no persona block at all while the plane is
// off. This asserts the injected bytes, not just the resolver's return value.
func TestForwardBodyCarriesNoPersonaWhilePlaneOff(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	// Drop the shared DB's connections so this test's mock upstream is the only
	// reachable one; otherwise the seeded deepseek connection wins and the
	// request leaves for the real api.deepseek.com.
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatal(err)
	}

	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-plane-off", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	seedPersonaPlane(t, repo, true, "",
		persona.Persona{ID: "explicit", SystemPrompt: "PLANE-OFF-MARKER persona text"},
	)
	if err := repo.SetFeatureFlag(flagPersonaPlane, false); err != nil {
		t.Fatal(err)
	}
	h := NewChatHandler(repo)

	body := `{"model":"deepseek/deepseek-chat","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(PersonaHeader, "explicit")
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request should still be served: %d %s", rec.Code, rec.Body.String())
	}

	if strings.Contains(string(received), "PLANE-OFF-MARKER") {
		t.Fatalf("persona text reached upstream while the plane was off: %s", received)
	}
	var sent map[string]any
	if err := json.Unmarshal(received, &sent); err != nil {
		t.Fatal(err)
	}
	messages := sent["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("body gained a system message while the plane was off: %s", received)
	}

	// With the flag back on the same request does carry the persona, so the
	// assertion above is testing the flag and not an unrelated failure.
	if err := repo.SetFeatureFlag(flagPersonaPlane, true); err != nil {
		t.Fatal(err)
	}
	on := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	on.Header.Set(PersonaHeader, "explicit")
	onRec := httptest.NewRecorder()
	h.HandleChatCompletions(onRec, on)
	if onRec.Code != http.StatusOK {
		t.Fatalf("flag-on request failed: %d %s", onRec.Code, onRec.Body.String())
	}
	if !strings.Contains(string(received), "PLANE-OFF-MARKER") {
		t.Fatalf("flag on must attach the persona: %s", received)
	}
}

// The RTK off-state must hold through the real forward path, not just the unit
// that compresses: the body that leaves for the provider must be the body the
// client sent.
func TestForwardBodyUnchangedWhileRTKFlagOff(t *testing.T) {
	database, cleanup := setupChatTestDB(t)
	defer cleanup()
	// Drop the shared DB's connections so this test's mock upstream is the only
	// reachable one.
	if _, err := database.Exec(`DELETE FROM providerConnections`); err != nil {
		t.Fatal(err)
	}

	var received []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()

	seedConnDB(t, database, "deepseek", "conn-rtk-flag", "sk-test", upstream.URL)
	repo := db.NewRepo(database)
	// Process-level config has RTK on, so only the flag can explain the result.
	h := NewChatHandler(repo, shared.NewTokenSaverConfig(true, false, false))

	sent := func() string {
		t.Helper()
		var sb strings.Builder
		for i := range 300 {
			sb.WriteString("unique log line number ")
			sb.WriteString(strconv.Itoa(i))
			sb.WriteString("\n")
		}
		payload, err := json.Marshal(map[string]any{
			"model":    "deepseek/deepseek-chat",
			"messages": []any{map[string]any{"role": "tool", "content": sb.String()}},
		})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(payload)))
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request failed: %d %s", rec.Code, rec.Body.String())
		}
		return string(received)
	}

	// The middle of the payload is the honest probe: RTK keeps a head and a tail
	// of the text it compresses, so the first and last lines survive either way
	// and only an interior line distinguishes compressed from untouched.
	const middleMarker = "unique log line number 150"

	// Flag on: the tool payload is compressed before it leaves.
	compressed := sent()
	if strings.Contains(compressed, middleMarker) {
		t.Fatalf("RTK flag on should compress the tool payload: %s", compressed)
	}

	// Flag off: the payload reaches upstream intact, interior lines included.
	if err := repo.SetFeatureFlag(flagRTKSaver, false); err != nil {
		t.Fatal(err)
	}
	raw := sent()
	if !strings.Contains(raw, middleMarker) {
		t.Fatalf("RTK flag off must forward the body unchanged: %s", raw)
	}
}
