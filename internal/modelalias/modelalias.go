// Package modelalias stores operator-declared model bindings: a model name that
// resolves to another model and optionally carries a persona.
//
// This is the mechanism behind names like "glm-5.3-mod": a client sends the
// bound name, the gateway rewrites it to the target and splices the persona into
// the outbound system prompt. Nothing about the upstream weights changes — the
// binding is configuration, and the persona cost is paid on every call that
// names it.
//
// Renaming alone is already handled by the repository's kv alias table
// (scope "modelAliases"). A binding exists for the part that table cannot
// express: attaching prompt context to a model name, so a client that cannot
// send a system prompt — or an operator who wants the persona applied without
// touching client config — can select it by model id alone.
//
// Two properties are deliberate:
//
//   - A binding never shadows a real catalog model silently: resolution consults
//     bindings before the catalog, so an operator who binds an existing name
//     redirects that traffic on purpose and sees the rewrite in the log.
//   - A disabled binding resolves to nothing. The name is then an unknown model,
//     which is a visible error rather than a silent fallback to the target.
package modelalias

import (
	"fmt"
	"regexp"
)

const (
	// MaxIDLength bounds the bound name. It matches the persona and bounty
	// profile bound so one naming rule covers every operator-defined key.
	MaxIDLength = 64
	// MaxTargetLength bounds the target model string.
	MaxTargetLength = 200
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)

// ValidID reports whether id is a well-formed binding key. A slash is allowed so
// an operator can group bindings under a prefix ("team/glm-mod"), which is how
// provider-qualified names are written.
func ValidID(id string) bool {
	return id != "" && len(id) <= MaxIDLength && idPattern.MatchString(id)
}

// Binding is one operator-declared model binding.
//
// At least one of Target or Persona must be set: a binding that changes neither
// the model nor the prompt is a no-op that would only confuse resolution. Target
// is optional because the existing kv alias table may already perform the
// rename; Persona is optional because a binding may exist purely to rename.
type Binding struct {
	// ID is the model name clients send.
	ID string `json:"id"`
	// Target is the model the request is rewritten to, in the form the client
	// could have sent ("model" or "provider/model"). Empty leaves the model name
	// to the existing alias and catalog resolution.
	Target string `json:"target,omitempty"`
	// Persona is the persona id spliced into the outbound system prompt. Empty
	// means the binding carries no prompt context.
	Persona string `json:"persona,omitempty"`
	// Enabled turns the binding on. A disabled binding resolves to nothing.
	Enabled bool `json:"enabled"`
}

// Validate rejects an incomplete or malformed binding before it is stored or
// used.
//
// The raw ID must be well-formed and used exactly as given: the repository keys
// bindings by the exact string, so accepting " padded " would persist a key no
// request can select.
func (b Binding) Validate() error {
	if !ValidID(b.ID) {
		return fmt.Errorf("invalid model binding id %q: use letters, digits, dot, dash, underscore or slash, up to %d characters", b.ID, MaxIDLength)
	}
	if b.Target == "" && b.Persona == "" {
		return fmt.Errorf("binding %q does nothing: set a target model, a persona, or both", b.ID)
	}
	if len(b.Target) > MaxTargetLength {
		return fmt.Errorf("binding %q target exceeds %d bytes", b.ID, MaxTargetLength)
	}
	if b.Target == b.ID {
		return fmt.Errorf("binding %q cannot target itself", b.ID)
	}
	if b.Persona != "" && !personaIDLooksValid(b.Persona) {
		return fmt.Errorf("binding %q names an invalid persona id %q", b.ID, b.Persona)
	}
	return nil
}

// personaIDLooksValid mirrors the persona package's id rule. It is duplicated
// rather than imported so this package keeps no storage dependency; the persona
// package validates the id again when the binding is used, so a mismatch cannot
// smuggle an unusable id through.
func personaIDLooksValid(id string) bool {
	if id == "" || len(id) > MaxIDLength {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ResolveTarget returns the model this binding rewrites to, and whether a
// rewrite applies. A disabled binding or one without a target rewrites nothing.
func (b Binding) ResolveTarget() (string, bool) {
	if !b.Enabled || b.Target == "" {
		return "", false
	}
	return b.Target, true
}

// PersonaID returns the persona this binding selects, and whether one applies.
// A disabled binding selects no persona, so turning a binding off removes its
// prompt context without deleting the record.
func (b Binding) PersonaID() (string, bool) {
	if !b.Enabled || b.Persona == "" {
		return "", false
	}
	return b.Persona, true
}
