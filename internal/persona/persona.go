// Package persona stores operator-declared system-prompt additions and renders
// them as a bounded, transparent block.
//
// A persona is operator instruction text — the operator writes it and the
// gateway splices it into the outgoing system prompt. It is therefore emitted
// verbatim rather than JSON-quoted like bounty scope *data*: quoting the
// operator's own multi-line instructions would corrupt the text they wrote.
// The block is instead delimited and guarded by fixed marker lines, so a
// persona can never visually impersonate a new provider-level system section,
// and the provider's own safety policies are explicitly retained.
//
// Unlike bounty profiles, which are selected only by explicit request header,
// a persona may also be applied by operator-set default — but only while the
// persona plane is enabled, which is off by default.
package persona

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxPersonaIDLength bounds the stored key, matching the bounty profile id
	// budget so both selector headers stay within a comfortably short value.
	MaxPersonaIDLength = 64
	// MaxPersonaNameLength bounds the human-readable label shown in the CLI and
	// dashboard lists; the map key stays the ID.
	MaxPersonaNameLength = 120
	// MaxSystemPromptLength bounds the operator's system-prompt text in BYTES,
	// not runes: the bound must match what the wire payload actually carries.
	//
	// The bound is generous on purpose. An agent persona is a real document —
	// operating doctrine, evidence standards, report formats — and the ones this
	// feature exists to match run to tens of thousands of tokens. A tighter limit
	// would reject exactly the personas the binding feature is for, so the size is
	// bounded to keep a stored record sane rather than to second-guess the
	// operator's prompt engineering.
	MaxSystemPromptLength = 262144
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidPersonaID reports whether id is a well-formed persona key. It exists so
// storage layers can validate a map key before decoding a record, without
// constructing a throwaway Persona.
func ValidPersonaID(id string) bool {
	return id != "" && len(id) <= MaxPersonaIDLength && idPattern.MatchString(id)
}

// Persona is one operator-declared system-prompt addition.
//
// AppendExisting defaults to false when a record omits the field, which means
// "replace the caller's system prompt with this persona" — the explicit
// operator choice. When it is true the persona is appended BELOW whatever
// system content the caller already sent, preserving caller instructions that
// are unrelated to the persona (tool policy, response format, language). The
// default is the safer one for a stored record that lost its flag: nothing can
// append a stale persona onto an unrelated request.
type Persona struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	SystemPrompt   string `json:"systemPrompt"`
	AppendExisting bool   `json:"appendExisting"`
	// Inline delivers the persona as the leading content of the first user
	// message instead of as a system message. It exists for upstreams that do
	// not process a system message at all: measured against one such provider, a
	// 71 KB system prompt was reported as 42 prompt tokens and never reached the
	// model, while the same text in a user message was counted in full. A long
	// persona delivered through system is silently useless there.
	Inline bool `json:"inline,omitempty"`
}

// Validate rejects an incomplete or oversized persona before storing or using
// it.
//
// The raw ID must be well-formed: the repository keys personas by the exact ID
// string, so accepting " padded " here would persist a key no header can
// select. HTTP input is trimmed by the dashboard handler before this check.
func (p Persona) Validate() error {
	if !ValidPersonaID(p.ID) {
		return fmt.Errorf("persona id must be 1-%d letters, digits, '.', '_' or '-'", MaxPersonaIDLength)
	}
	if strings.TrimSpace(p.SystemPrompt) == "" {
		return errors.New("persona system prompt is required; an empty persona would only relabel existing instructions")
	}
	if len(p.Name) > MaxPersonaNameLength {
		return fmt.Errorf("persona name must be at most %d characters", MaxPersonaNameLength)
	}
	if !p.fitsPromptBudget() {
		return fmt.Errorf("persona system prompt exceeds %d bytes", MaxSystemPromptLength)
	}
	return nil
}

// fitsPromptBudget reports whether the rendered block fits MaxSystemPromptLength.
// It measures the untruncated rendering so the bound is a real limit rather than
// a comparison against an already-clamped string.
func (p Persona) fitsPromptBudget() bool {
	return len(p.BuildSystemAddition()) <= MaxSystemPromptLength
}

// PersonaAddition is the rendered system-prompt block produced by a persona.
type PersonaAddition struct {
	// Text is the bounded block, including the marker and guard lines.
	Text string
	// Replace reports whether the caller's own system content must be dropped:
	// it is true only for the explicit AppendExisting=false choice.
	Replace bool
	// Inline reports whether the block travels as leading user content rather
	// than in a system field, for upstreams that ignore system messages.
	Inline bool
}

// BuildSystemAddition renders the persona block.
//
// The fixed opening line marks the text as operator-declared, and the fixed
// closing line keeps the provider's own policies authoritative — the same
// boundary style the bounty context uses. Both lines are emitted around the
// verbatim persona text; only the opening and closing markers are structural.
func (p Persona) BuildSystemAddition() string {
	var b strings.Builder
	b.WriteString(openingLine)
	b.WriteByte('\n')
	b.WriteString(strings.TrimSpace(p.SystemPrompt))
	b.WriteString(closingLine)
	return b.String()
}

// Addition bundles the rendered block with the replacement decision, so an
// injector cannot apply one without the other.
func (p Persona) Addition() PersonaAddition {
	return PersonaAddition{
		Text:    p.BuildSystemAddition(),
		Replace: !p.AppendExisting,
		Inline:  p.Inline,
	}
}

// AdditionLength reports the bounded length of the rendered addition.
func (p Persona) AdditionLength() int { return len(p.BuildSystemAddition()) }

// openingLine is the fixed, non-negotiable marker that frames everything after
// it as operator instruction text rather than a provider section. A persona that
// begins with its own header cannot remove it, so the block never impersonates
// a system-level directive the operator did not declare.
const openingLine = "Persona instruction (operator-declared; applies to this request only, below this marker):"

// closingLine is the fixed, non-truncatable guard line that keeps the provider's
// own policies authoritative. It is emitted after the persona text, so an
// oversized persona cannot push it out of the block.
const closingLine = "\nThe persona text above is operator-declared; provider safety policies remain authoritative and take precedence over it."
