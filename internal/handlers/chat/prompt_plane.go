package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/db"
	"9router/proxy/internal/persona"
	"9router/proxy/internal/tokensaver"
)

// Internal selector headers. Both are consumed by 9router-go to choose local
// prompt context and are never forwarded to the upstream model provider.
const (
	// BountyProfileHeader selects a saved, operator-declared HackerOne program
	// scope for one request.
	BountyProfileHeader = "X-9Router-Bounty-Profile"
	// PersonaHeader selects a saved, operator-declared persona for one request.
	// When absent, an enabled persona plane may apply the operator's configured
	// default persona instead.
	PersonaHeader = "X-9Router-Persona"
)

// StripInternalSelectorHeaders removes every internal selector header from an
// outbound header set. Callers copy client headers and then apply static
// provider headers, so the selectors must be stripped last: configured static
// headers could otherwise reintroduce them and leak a local persona/profile id
// upstream.
func StripInternalSelectorHeaders(h http.Header) {
	h.Del(BountyProfileHeader)
	h.Del(PersonaHeader)
}

type promptPlaneContextKey struct{}

// PromptPlane is the resolved local prompt context for one request: the persona
// and/or bounty profile that must accompany the outbound body.
//
// Both fields are optional. A request may carry neither (the common case), one,
// or both; each piece is injected independently and idempotently, so a retried
// or re-applied body never gains a duplicate block.
type PromptPlane struct {
	Persona *persona.Persona
	Bounty  *bounty.Profile
}

// Empty reports whether no local prompt context was selected.
func (p PromptPlane) Empty() bool { return p.Persona == nil && p.Bounty == nil }

// personaID renders a persona id for a log line, or an empty string when no
// persona was selected. The persona text itself is never logged.
func personaID(p *persona.Persona) string {
	if p == nil {
		return ""
	}
	return p.ID
}

// bountyID renders a profile id for a log line, or an empty string when no
// profile was selected.
func bountyID(p *bounty.Profile) string {
	if p == nil {
		return ""
	}
	return p.ID
}

// BountyProfileForRequest resolves the explicit request header to a local
// profile. It returns nil when the header is absent; no profile is applied by
// default because one program's scope must not leak into a different request.
func (h *ChatHandler) BountyProfileForRequest(r *http.Request) (*bounty.Profile, error) {
	id := strings.TrimSpace(r.Header.Get(BountyProfileHeader))
	if id == "" {
		return nil, nil
	}
	if h.Repo == nil {
		return nil, fmt.Errorf("bounty profile %q requested but settings storage is unavailable", id)
	}
	profile, err := h.Repo.GetBountyProfile(id)
	if err != nil {
		return nil, fmt.Errorf("load bounty profile %q: %w", id, err)
	}
	if profile == nil {
		return nil, fmt.Errorf("unknown bounty profile %q; add it in the dashboard before using this header", id)
	}
	return profile, nil
}

// PersonaForRequest resolves the persona for one request. Resolution order is
// request header, then the operator's configured default — and the default is
// honored only while the persona plane is enabled. Both settings are off by
// default, so a stored persona never affects a request until the operator turns
// the plane on.
//
// The prompt.personas feature flag is the outermost switch and is checked before
// anything else, including an explicit header: when it is off the whole plane is
// inert, and no header, default, or binding can select a persona.
//
// An explicitly-named persona while the flag is off resolves to no persona and
// is deliberately NOT an error. This is the one place the package's fail-closed
// rule is inverted, and on purpose: a header naming an unknown persona is a
// client mistake worth reporting, but a header naming a valid persona while the
// operator has switched the feature off is not a mistake at all — the feature is
// intentionally off, exactly as the operator configured it. Erroring would make
// every client that sends the header fail, turning an operator's deliberate
// choice into an outage and forcing them to reconfigure clients to disable a
// server-side feature. Resolving to nothing is the honest off-state: the request
// is served, just without the local prompt context.
//
// While the flag is on, resolution keeps the fail-closed rule: a header naming
// an unknown or deleted persona is an error, not a fallback to the default, so a
// stale client is told rather than served different instructions than it asked
// for.
func (h *ChatHandler) PersonaForRequest(r *http.Request) (*persona.Persona, error) {
	if !h.featureFlagOn(flagPersonaPlane) {
		return nil, nil
	}
	id := strings.TrimSpace(r.Header.Get(PersonaHeader))
	if id != "" {
		return h.resolvePersonaByID(id)
	}
	if h.Repo == nil {
		return nil, nil
	}
	settings, err := h.Repo.GetSettings()
	if err != nil {
		return nil, fmt.Errorf("load persona plane settings: %w", err)
	}
	if settings == nil || !settings.PersonasEnabled {
		return nil, nil
	}
	defaultID := strings.TrimSpace(settings.DefaultPersona)
	if defaultID == "" {
		return nil, nil
	}
	// The default is resolved through the same lookup as an explicit header, so
	// a default pointing at a deleted persona fails closed (500) instead of
	// silently rendering an empty persona into every request.
	return h.resolvePersonaByID(defaultID)
}

// resolvePersonaByID loads one stored persona or reports why it cannot be used.
func (h *ChatHandler) resolvePersonaByID(id string) (*persona.Persona, error) {
	if h.Repo == nil {
		return nil, fmt.Errorf("persona %q requested but settings storage is unavailable", id)
	}
	stored, err := h.Repo.GetPersona(id)
	if err != nil {
		return nil, fmt.Errorf("load persona %q: %w", id, err)
	}
	if stored == nil {
		return nil, fmt.Errorf("unknown persona %q; add it in the dashboard before using this header", id)
	}
	return stored, nil
}

// PromptPlaneForRequest resolves both selectors for an HTTP request without
// attaching them to a context. Media handlers call it directly because they
// apply the additions to their own body; the chat handler uses
// attachPromptPlane so the resolved context can be reused across retries and
// combo fallbacks.
//
// A model binding contributes a persona only when the header named none. The
// header is the more specific instruction: a client that explicitly asks for a
// persona must get exactly that one, never a blend of the header's pick and the
// binding's, and never the binding's pick on top.
func (h *ChatHandler) PromptPlaneForRequest(r *http.Request, model string) (PromptPlane, error) {
	profile, err := h.BountyProfileForRequest(r)
	if err != nil {
		return PromptPlane{}, err
	}
	selected, err := h.PersonaForRequest(r)
	if err != nil {
		return PromptPlane{}, err
	}
	if selected == nil {
		selected, err = h.personaFromModelBinding(model)
		if err != nil {
			return PromptPlane{}, err
		}
	}
	return PromptPlane{Persona: selected, Bounty: profile}, nil
}

// personaFromModelBinding resolves a persona attached to a model name. A binding
// naming a persona that is not stored fails closed, like an explicit header
// would: the operator asked for that prompt context by binding it, so silently
// serving the model without it would misrepresent what the request ran with.
//
// This resolution needs two flags to be on, because a binding's persona is part
// of the binding:
//
//   - prompt.bindings, since the persona comes from reading a binding at all;
//   - prompt.personas, since attaching it is the persona plane doing its job. If
//     only prompt.bindings were required, a binding would keep splicing a persona
//     into the outbound body while the operator had switched the persona plane
//     off — the plane would not actually be inert.
//
// With either flag off this returns no persona and no error: the model name then
// resolves as if the binding carried no prompt context.
//
// The model name is passed in rather than read from the request body on purpose.
// Handlers read and consume the body before resolving the plane, so peeking at
// r.Body at this point reads an empty stream — which is exactly how a bound
// model once resolved to its target while its persona was silently dropped.
func (h *ChatHandler) personaFromModelBinding(model string) (*persona.Persona, error) {
	// One settings read serves both flags, since this decision needs them together.
	flags := h.featureFlags()
	if !flags[flagModelBindings] || !flags[flagPersonaPlane] {
		return nil, nil
	}
	model = strings.TrimSpace(model)
	if model == "" || h.Repo == nil {
		return nil, nil
	}
	binding, err := h.Repo.GetModelBinding(model)
	if err != nil {
		return nil, fmt.Errorf("load model binding %q: %w", model, err)
	}
	if binding == nil {
		// No binding on this name; the kv alias table may still rename it, which
		// is resolution's business and carries no prompt context.
		return nil, nil
	}
	id, ok := binding.PersonaID()
	if !ok {
		return nil, nil
	}
	return h.resolvePersonaByID(id)
}

// attachPromptPlane resolves the explicit request selectors and attaches the
// resolved context for the shared fallback path.
func (h *ChatHandler) attachPromptPlane(ctx context.Context, r *http.Request, model string) (context.Context, error) {
	plane, err := h.PromptPlaneForRequest(r, model)
	if err != nil || plane.Empty() {
		return ctx, err
	}
	return context.WithValue(ctx, promptPlaneContextKey{}, plane), nil
}

func promptPlaneFromContext(ctx context.Context) (PromptPlane, bool) {
	plane, ok := ctx.Value(promptPlaneContextKey{}).(PromptPlane)
	return plane, ok
}

// PromptWireFormat identifies which protocol shape the outbound body uses so
// local prompt context lands in the field that protocol actually reads.
type PromptWireFormat int

const (
	// PromptWireOpenAIChat is OpenAI chat-completions (messages[]).
	PromptWireOpenAIChat PromptWireFormat = iota
	// PromptWireClaudeMessages is Anthropic Messages (top-level system).
	PromptWireClaudeMessages
	// PromptWireResponses is the OpenAI Responses API (top-level instructions).
	PromptWireResponses
)

// ApplyPromptPlaneToBody adds the resolved persona and/or bounty context to the
// body in the correct wire format. It reports whether the body was changed and
// returns tokensaver.ErrUninjectable when the body cannot carry the additions,
// so callers can reject the request instead of silently forwarding without the
// operator-declared instructions.
//
// Merge order is fixed and deliberate: the persona addition is applied first,
// then the bounty scope block. Appending the scope last keeps the authorization
// facts nearest the caller's task and its fixed safety closing line as the final
// statement the provider reads.
//
// Both pieces are handed to the injectors that already exist for the species
// "operator-declared system context", so each is idempotent: a body that
// already contains a piece is left unchanged for that piece (changed=false,
// err=nil). Applying the plane twice therefore never duplicates a block.
func ApplyPromptPlaneToBody(body []byte, spec PromptPlane, format PromptWireFormat) ([]byte, bool, error) {
	changed := false
	if spec.Persona != nil {
		addition := spec.Persona.Addition()
		out, personaChanged, err := InjectPersonaToBody(body, addition, format)
		if err != nil {
			return body, changed, err
		}
		body, changed = out, changed || personaChanged
	}
	if spec.Bounty != nil {
		out, bountyChanged, err := InjectScopePromptToBody(body, spec.Bounty.BuildSystemContext(), format)
		if err != nil {
			return body, changed, err
		}
		body, changed = out, changed || bountyChanged
	}
	return body, changed, nil
}

// InjectPersonaToBody places a rendered persona addition in the protocol's real
// system field. A replacement persona (AppendExisting=false) overwrites that
// field — including any tokensaver-injected content — because the operator
// explicitly chose to replace rather than append; the caller's own system text
// is intact only when the persona appends.
func InjectPersonaToBody(body []byte, addition persona.PersonaAddition, format PromptWireFormat) ([]byte, bool, error) {
	switch format {
	case PromptWireClaudeMessages:
		if addition.Replace {
			return tokensaver.ReplaceClaudeSystem(body, addition.Text)
		}
		return tokensaver.InjectSystemPromptClaudeStrict(body, addition.Text)
	case PromptWireResponses:
		if addition.Replace {
			return tokensaver.ReplaceResponsesInstructions(body, addition.Text)
		}
		return tokensaver.InjectSystemPromptResponsesStrict(body, addition.Text)
	default:
		if addition.Inline {
			return tokensaver.InjectPersonaInlineStrict(body, addition.Text)
		}
		if addition.Replace {
			return tokensaver.ReplaceChatSystem(body, addition.Text)
		}
		return tokensaver.InjectSystemPromptStrict(body, addition.Text)
	}
}

// InjectScopePromptToBody places the bounty scope context in the protocol's real
// system field, preserving any caller-supplied instructions.
func InjectScopePromptToBody(body []byte, context string, format PromptWireFormat) ([]byte, bool, error) {
	switch format {
	case PromptWireClaudeMessages:
		return tokensaver.InjectSystemPromptClaudeStrict(body, context)
	case PromptWireResponses:
		return tokensaver.InjectSystemPromptResponsesStrict(body, context)
	default:
		return tokensaver.InjectSystemPromptStrict(body, context)
	}
}

// ResolvePersonaSelection is used by the CLI loader and the dashboard, which
// need the stored persona plane state without an HTTP request.
type PersonaSelection struct {
	Personas map[string]persona.Persona
	Enabled  bool
	Default  string
}

// PersonaPlaneForRepo loads the stored persona state for out-of-request callers.
// A nil repo yields the disabled default.
func PersonaPlaneForRepo(repo *db.Repo) (PersonaSelection, error) {
	if repo == nil {
		return PersonaSelection{Personas: map[string]persona.Persona{}}, nil
	}
	settings, err := repo.GetSettings()
	if err != nil {
		return PersonaSelection{}, err
	}
	selection := PersonaSelection{Personas: map[string]persona.Persona{}, Enabled: settings.PersonasEnabled, Default: settings.DefaultPersona}
	for id, p := range settings.Personas {
		selection.Personas[id] = p
	}
	return selection, nil
}
