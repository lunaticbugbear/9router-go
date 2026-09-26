// Package featureflags is the single registry of every optional capability
// the gateway can expose.
//
// The registry exists so that turning a capability on or off is one switch in
// one place, rather than an env var in one handler and a settings row in
// another drifting apart. Each flag names a real, bounded behavior — no flag
// is a promise to build something; a flag is the operator's choice about a
// behavior that exists.
//
// Flags are deliberately coarse: they gate features, not policies inside them.
// A disabled flag means the feature's routes, CLI subcommands, and dashboard
// surfaces do nothing — the request plane keeps working exactly as it did
// before the flag existed.
package featureflags

import "sync"

// Flag is one switchable capability.
type Flag struct {
	// ID is the stable key stored in settings. Never rename one: a renamed ID
	// silently resets the operator's choice to the default.
	ID string
	// Title is the short name shown in the dashboard.
	Title string
	// Description is one or two sentences: what the operator gets, and what
	// changes when it is on. Written for the operator deciding, not for docs.
	Description string
	// Default is the fresh-install state. Features that are exploratory, that
	// cost tokens on every request, or that change outbound bodies default to
	// off; conveniences and read-only views default to on.
	Default bool
	// Category groups flags in the dashboard so 41 switches stay scannable.
	Category string
	// Stage reports the flag's honesty level: "stable" for wired behaviors,
	// "planned" for a registered future capability whose toggle has no effect
	// until its feature lands. A planned flag is still worth registering now,
	// because the settings menu then becomes the roadmap — but it must say so.
	Stage Stage
}

// Stage is the honesty label for a flag.
type Stage int

const (
	// Stable means the toggle controls live behavior today.
	Stable Stage = iota
	// Planned means the capability is registered but not yet built; flipping
	// the toggle changes nothing yet. The dashboard shows this plainly.
	Planned
)

func (s Stage) String() string {
	if s == Planned {
		return "planned"
	}
	return "stable"
}

// Categories, in dashboard display order. A flag with an unknown category is
// still served — the dashboard falls back to "other" — but keeping categories
// literal here makes the grouping deliberate rather than accidental.
const (
	CategoryRouting      = "Routing"
	CategoryTokens       = "Token savers"
	CategoryObservation  = "Observation"
	CategoryPrompt       = "Prompt plane"
	CategorySecurity     = "Security"
	CategoryOnboarding   = "Install & operations"
	CategoryProtocols    = "API surface"
	CategorySubscription = "Subscription accounts"
)

// registry is append-only. Order here is the order the dashboard renders.
var registry = []Flag{
	// --- Routing ------------------------------------------------------------
	{ID: "routing.combos", Title: "Model combos", Description: "Route one model name across several providers with fallback, rotation, or fusion strategies. Turning this off makes every name resolve to exactly one target.", Default: true, Category: CategoryRouting},
	{ID: "routing.multi-account", Title: "Multi-account rotation", Description: "Round-robin between multiple credentials of the same provider so one account's quota is not drained first.", Stage: Planned, Category: CategoryRouting},
	{ID: "routing.sticky-sessions", Title: "Sticky sessions", Description: "Keep requests from one client session on the same upstream account, so cached context and rate buckets stay warm.", Default: true, Category: CategoryRouting},
	{ID: "routing.conditional", Title: "Conditional routing", Description: "Reroute by request shape: long contexts to wide-window models, images to vision-capable ones.", Stage: Planned, Category: CategoryRouting},
	{ID: "routing.performance-learning", Title: "Performance-aware routing", Description: "Prefer the upstream that actually answered fastest and cleanest in your own traffic, measured from request history.", Stage: Planned, Category: CategoryRouting},
	{ID: "routing.quota-aware", Title: "Quota-aware scheduling", Description: "Spread load across accounts in proportion to each one's remaining quota instead of draining them in order.", Stage: Planned, Category: CategoryRouting},

	// --- Token savers -------------------------------------------------------
	{ID: "tokensavers.rtk", Title: "RTK compression", Description: "Compress tool_result content before sending it upstream. Saves tokens on coding traffic that is dominated by tool output.", Default: true, Category: CategoryTokens},
	{ID: "tokensavers.caveman", Title: "Caveman compression", Description: "Aggressively compress prompts to a terse form. Bigger savings, more visible style change in responses.", Stage: Planned, Category: CategoryTokens},
	{ID: "tokensavers.verified-savings", Title: "Verified savings report", Description: "Measure what the savers actually saved per request from your own history, instead of trusting a percentage claim.", Stage: Planned, Category: CategoryTokens},

	// --- Observation --------------------------------------------------------
	{ID: "observation.session-tracing", Title: "Session tracing", Description: "Group requests by client session and show cost, latency, and model use per session in the dashboard.", Default: true, Category: CategoryObservation},
	{ID: "observation.identity-drift", Title: "Identity drift detection", Description: "Flag upstreams that answer a model name with a different identity than they did before — a sign of silent backend switching.", Stage: Planned, Category: CategoryObservation},
	{ID: "observation.capability-ledger", Title: "Provider capability ledger", Description: "Record per provider what has actually been proven: system-prompt support, token accounting honesty, real context window. Routing decisions consult it.", Stage: Planned, Category: CategoryObservation},
	{ID: "observation.bodies", Title: "Store request bodies", Description: "Keep full request/response bodies in the log for replay and debugging. Off by default: bodies can contain your code and secrets.", Stage: Planned, Category: CategoryObservation},
	{ID: "observation.playground", Title: "Dashboard playground", Description: "Try models, prompts, and personas from the dashboard before committing them to a binding.", Stage: Planned, Category: CategoryObservation},
	{ID: "observation.model-audit", Title: "Model catalog audit", Description: "Show which advertised context windows are rule-based and which are fallback guesses.", Default: true, Category: CategoryObservation},

	// --- Prompt plane -------------------------------------------------------
	{ID: "prompt.personas", Title: "Persona loader", Description: "Operator-declared system-prompt additions selected by header, default, or model binding. The core of persona-bound model names. Turning this off makes the whole plane inert: no selector applies a persona.", Default: true, Category: CategoryPrompt},
	{ID: "prompt.bindings", Title: "Persona-bound model names", Description: "Create model names that carry a persona automatically, like a provider's -mod variants but under your control. Turning this off stops the rewrite a binding performs, so a bound name falls back to alias and catalog resolution.", Default: true, Category: CategoryPrompt},
	{ID: "prompt.bounty", Title: "Bounty authorization context", Description: "Attach your declared bug-bounty program scope to requests so providers can distinguish authorized testing from unscoped probing.", Default: true, Category: CategoryPrompt},
	{ID: "prompt.guardrails", Title: "Declarative guardrails", Description: "Define input and output checks with deny or retry behavior, instead of relying only on the injection guard flag.", Stage: Planned, Category: CategoryPrompt},
	{ID: "prompt.versioning", Title: "Persona versioning", Description: "Keep a history of every persona edit and roll back a bad change from the dashboard or CLI.", Stage: Planned, Category: CategoryPrompt},

	// --- Security -----------------------------------------------------------
	{ID: "security.encryption-at-rest", Title: "Encrypt stored credentials", Description: "Encrypt provider API keys and OAuth tokens in the database with AES-256-GCM, plus a rekey command for rotation.", Stage: Planned, Category: CategorySecurity},
	{ID: "security.virtual-keys", Title: "Virtual keys and budgets", Description: "Issue gateway keys with their own spend caps and model allowlists so a leaked or shared key cannot drain everything.", Stage: Planned, Category: CategorySecurity},
	{ID: "security.token-rate-limits", Title: "Token-level rate limits", Description: "Throttle by tokens per minute, not just requests per minute.", Stage: Planned, Category: CategorySecurity},
	{ID: "security.concurrency-limits", Title: "Concurrency limits", Description: "Cap simultaneous requests per gateway key and per upstream account.", Stage: Planned, Category: CategorySecurity},
	{ID: "security.oidc", Title: "Single sign-on (OIDC)", Description: "Log into the dashboard with an identity provider instead of the admin password.", Stage: Planned, Category: CategorySecurity},

	// --- Install & operations -------------------------------------------------
	{ID: "ops.setup-wizard", Title: "Setup wizard", Description: "First-run wizard that walks through schema, provider, and key creation instead of requiring CLI steps.", Stage: Planned, Category: CategoryOnboarding},
	{ID: "ops.dashboard-upgrade", Title: "In-dashboard upgrade", Description: "Check for updates and apply or roll back from the dashboard.", Stage: Planned, Category: CategoryOnboarding},
	{ID: "ops.mobile-friendly", Title: "Mobile admin surface", Description: "Keep the admin API clean so a mobile console remains buildable.", Stage: Planned, Category: CategoryOnboarding},

	// --- API surface ----------------------------------------------------------
	{ID: "api.batches", Title: "Batch endpoint", Description: "/v1/batches for asynchronous multi-request jobs across providers.", Stage: Planned, Category: CategoryProtocols},
	{ID: "api.rerank", Title: "Rerank endpoint", Description: "Normalized /v1/rerank across providers that support it.", Stage: Planned, Category: CategoryProtocols},
	{ID: "api.a2a", Title: "A2A agent protocol", Description: "Invoke external agents through the gateway as if they were models.", Stage: Planned, Category: CategoryProtocols},
	{ID: "api.mcp", Title: "MCP proxy", Description: "Proxy Model Context Protocol servers with one auth layer and per-tool access control.", Stage: Planned, Category: CategoryProtocols},
	{ID: "api.embeddings", Title: "Unified embeddings", Description: "One /v1/embeddings surface across providers.", Stage: Planned, Category: CategoryProtocols},
	{ID: "api.openapi", Title: "Published OpenAPI spec", Description: "Serve a generated OpenAPI document and reject PRs that change routes without updating it.", Stage: Planned, Category: CategoryProtocols},

	// --- Subscription accounts --------------------------------------------------
	{ID: "accounts.health-benching", Title: "Account health benching", Description: "Automatically sideline accounts that keep failing and probe them for recovery before returning them to rotation.", Stage: Planned, Category: CategorySubscription},
	{ID: "accounts.composite-groups", Title: "Composite model groups", Description: "Map one model name to several providers as an admin routing layer, distinct from persona bindings.", Stage: Planned, Category: CategorySubscription},
	{ID: "accounts.free-tier-ledger", Title: "Free-tier usage ledger", Description: "Track what your free tiers actually delivered, computed from your own request history.", Stage: Planned, Category: CategorySubscription},
}

var (
	mu          sync.RWMutex
	registryMap map[string]Flag
)

func init() {
	rebuildIndex()
}

func rebuildIndex() {
	m := make(map[string]Flag, len(registry))
	for _, f := range registry {
		m[f.ID] = f
	}
	mu.Lock()
	registryMap = m
	mu.Unlock()
}

// All returns every registered flag in display order. Callers must not mutate
// the returned slice.
func All() []Flag {
	out := make([]Flag, len(registry))
	copy(out, registry)
	return out
}

// Get returns one flag's definition. The second return is false when the ID is
// unknown, which callers should treat as "feature does not exist" rather than
// guessing a default.
func Get(id string) (Flag, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := registryMap[id]
	return f, ok
}

// Categories returns the distinct categories in registry order.
func Categories() []string {
	seen := make(map[string]bool)
	var out []string
	for _, f := range registry {
		if !seen[f.Category] {
			seen[f.Category] = true
			out = append(out, f.Category)
		}
	}
	return out
}
