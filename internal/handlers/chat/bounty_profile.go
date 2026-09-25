package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/tokensaver"
)

// BountyProfileHeader selects a saved, operator-declared HackerOne program
// scope for one request. It is consumed by 9router-go and is never forwarded to
// the upstream model provider.
const BountyProfileHeader = "X-9Router-Bounty-Profile"

// StripBountyProfileHeader removes the internal profile selector from an
// outbound header set. Callers copy client headers and then apply static
// provider headers, so the selector must be stripped last: configured static
// headers could otherwise reintroduce it and leak the internal profile id
// upstream.
func StripBountyProfileHeader(h http.Header) {
	h.Del(BountyProfileHeader)
}

type bountyProfileContextKey struct{}

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

// attachBountyProfile resolves the explicit request header and attaches the
// profile to context for the shared fallback path.
func (h *ChatHandler) attachBountyProfile(ctx context.Context, r *http.Request) (context.Context, error) {
	profile, err := h.BountyProfileForRequest(r)
	if err != nil || profile == nil {
		return ctx, err
	}
	return context.WithValue(ctx, bountyProfileContextKey{}, *profile), nil
}

func bountyProfileFromContext(ctx context.Context) (bounty.Profile, bool) {
	profile, ok := ctx.Value(bountyProfileContextKey{}).(bounty.Profile)
	return profile, ok
}

// BountyWireFormat identifies which protocol shape the outbound body uses so
// the scope context lands in the field that protocol actually reads.
type BountyWireFormat int

const (
	// BountyWireOpenAIChat is OpenAI chat-completions (messages[]).
	BountyWireOpenAIChat BountyWireFormat = iota
	// BountyWireClaudeMessages is Anthropic Messages (top-level system).
	BountyWireClaudeMessages
	// BountyWireResponses is the OpenAI Responses API (top-level instructions).
	BountyWireResponses
)

// ApplyBountyProfileToBody adds the profile to the body in the correct wire
// format. It reports whether the body was changed and returns
// tokensaver.ErrUninjectable when the body cannot carry the context, so callers
// can reject the request instead of silently forwarding without the
// operator-declared scope.
//
// The prompt is the operator-declared program context; it never overrides
// provider policies. A body that already contains the prompt is left untouched
// (changed=false, err=nil) so repeated application is idempotent.
func ApplyBountyProfileToBody(body []byte, profile bounty.Profile, format BountyWireFormat) ([]byte, bool, error) {
	prompt := profile.BuildSystemContext()
	switch format {
	case BountyWireClaudeMessages:
		return tokensaver.InjectSystemPromptClaudeStrict(body, prompt)
	case BountyWireResponses:
		return tokensaver.InjectSystemPromptResponsesStrict(body, prompt)
	default:
		return tokensaver.InjectSystemPromptStrict(body, prompt)
	}
}
