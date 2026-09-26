package chat

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/shared"
	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
	"9router/proxy/internal/proxy"
	"9router/proxy/internal/proxy/executor"
	"9router/proxy/internal/proxy/oauth"
)

// NewChatHandler creates a ChatHandler with the given repository and a streaming-capable HTTP client.
// Pass a TokenSaverConfig to enable token saver features, or nil for all-off defaults.
func NewChatHandler(repo *db.Repo, ts ...*shared.TokenSaverConfig) *ChatHandler {
	executor.RegisterAll()
	oauth.RegisterAll()
	cfg := &shared.TokenSaverConfig{}
	if len(ts) > 0 && ts[0] != nil {
		cfg = ts[0]
	}
	// Timeout: 0 is required so long SSE streams are not cut short, but a
	// ResponseHeaderTimeout bounds how long we wait for the upstream to
	// start responding — closing the "accept then go silent" gap without
	// killing a stream that has already begun.
	var transport http.RoundTripper
	if origTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		t := origTransport.Clone()
		t.ResponseHeaderTimeout = 2 * time.Minute
		transport = proxy.NewFallbackTransport(t)
	} else if fb, ok := http.DefaultTransport.(*proxy.FallbackTransport); ok {
		transport = fb
	} else {
		transport = proxy.NewFallbackTransport(http.DefaultTransport)
	}
	return &ChatHandler{
		Repo: repo,
		Client: &http.Client{
			Transport: transport,
			Timeout:   0, // no timeout for streaming support
		},
		TokenSaver:  cfg,
		stickyState: make(map[string]*comboStickyState),
	}
}

// ResolveModel resolves a model string through aliases, combos, and provider/model parsing.
// Exported so other handlers (media, responses, etc.) can resolve model names.
func (h *ChatHandler) ResolveModel(modelStr string) (*ModelInfo, error) {
	return h.resolveModel(modelStr)
}

// resolveProviderAlias resolves a provider alias to its canonical ID.
func resolveProviderAlias(alias string) string {
	if canonical, ok := providers.ProviderAliasMap[alias]; ok {
		return canonical
	}
	return alias
}

// errModelResolutionCycle marks a cycle the runtime resolution guard detected.
// It is a sentinel so a caller can tell "the stored configuration loops" apart
// from "this name has no provider" without matching on the message text.
var errModelResolutionCycle = errors.New("model resolution cycle detected")

// resolveVisits is the visited set threaded through one model-resolution
// attempt, so a cycle in the stored configuration is bounded and reported
// instead of recursing until the goroutine stack is exhausted.
//
// A stack overflow is a fatal error: recover() cannot intercept it, no deferred
// cleanup runs, and the entire process aborts. A cycle (alias -> combo -> alias,
// or combo -> combo) therefore used to take down the whole gateway and every
// in-flight stream on it, rather than failing the single request that named the
// cyclic model.
//
// The set is keyed by name, and the chain records the order names were entered
// so the error can name the loop. A bare depth counter is deliberately not the
// defense: it would cap legitimate deep nesting, and it could not tell an
// operator which names form the cycle.
type resolveVisits struct {
	seen  map[string]bool
	chain []string
}

func newResolveVisits() *resolveVisits {
	return &resolveVisits{seen: make(map[string]bool)}
}

// enter marks name as being resolved and reports a cycle when it already is.
func (v *resolveVisits) enter(name string) error {
	if v.seen[name] {
		return v.cycleError(name)
	}
	v.seen[name] = true
	v.chain = append(v.chain, name)
	return nil
}

// leave clears name once its subtree has been fully resolved, so the same name
// can legitimately appear on two sibling branches (a diamond) without being
// reported as a cycle. Entries are strictly nested, so the chain is a stack.
func (v *resolveVisits) leave(name string) {
	delete(v.seen, name)
	for i := len(v.chain) - 1; i >= 0; i-- {
		if v.chain[i] == name {
			v.chain = v.chain[:i]
			return
		}
	}
}

// cycleError names the names that form the loop, so an operator can find the
// alias or combo responsible.
func (v *resolveVisits) cycleError(name string) error {
	chain := make([]string, 0, len(v.chain)+1)
	chain = append(chain, v.chain...)
	chain = append(chain, name)
	return fmt.Errorf("%w: %s", errModelResolutionCycle, strings.Join(chain, " -> "))
}

// resolveModelEntry parses a single "provider/model" string into a ModelInfo
// without combo or alias resolution (used when iterating combo entries).
// If the entry has no "/" (i.e. it's a combo name), it resolves the combo
// and returns its first concrete model with the combined model list.
//
// The nested-combo lookup is gated on routing.combos as well as the top-level
// one in resolveModel: a name reaching this entry point must not expand a combo
// the operator switched off, or the flag would be honoured on one path and
// ignored on the other. With the flag off a nested combo name is not expanded
// and returns nil, so the caller's resolution fails rather than silently
// expanding.
func (h *ChatHandler) resolveModelEntry(entry string) *ModelInfo {
	return h.resolveModelEntryGuarded(entry, newResolveVisits())
}

// resolveModelEntryGuarded is resolveModelEntry with the resolution attempt's
// visited set threaded in, so a name that is already being resolved on this
// path is refused instead of expanded again.
func (h *ChatHandler) resolveModelEntryGuarded(entry string, v *resolveVisits) *ModelInfo {
	if !strings.Contains(entry, "/") {
		if h.Repo == nil {
			return nil
		}
		// Alias lookup first, and deliberately not gated on routing.combos: an
		// alias is not a combo. resolveModel resolves a slashless alias before it
		// consults the combo table (an alias is the operator's most specific
		// rename instruction), so entry resolution must agree or the two paths
		// disagree about what a bare name means.
		//
		// Without this, a combo whose leaf named a slashless alias was silently
		// skipped by rotation: resolveModelEntry returned nil for the alias name
		// while resolveModel resolved it, and the rotation loop's nil -> continue
		// dropped the operator's leaf with no error and no log.
		if aliasTarget, err := h.Repo.GetModelAlias(entry); err == nil && aliasTarget != "" && aliasTarget != entry {
			if err := v.enter(entry); err != nil {
				log.Warn("combo", "model resolution cycle detected; refusing to expand",
					"model", entry, "cycle", err.Error())
				return nil
			}
			defer v.leave(entry)
			return h.resolveModelEntryGuarded(aliasTarget, v)
		}
		if !h.featureFlagOn(flagRoutingCombos) {
			return nil
		}
		// Enter the name before expanding it. This is the recursion point that
		// the fusion judge path reaches (combo.go resolves the judge model
		// straight from settings, which is never validated): a combo whose
		// first entry is its own name used to recurse until the goroutine stack
		// was exhausted. A stack overflow is fatal — recover() cannot intercept
		// it and no deferred cleanup runs — so it took down the whole process
		// rather than failing the one request that named the bad judge model.
		if err := v.enter(entry); err != nil {
			log.Warn("combo", "model resolution cycle detected; refusing to expand",
				"model", entry, "cycle", err.Error())
			return nil
		}
		defer v.leave(entry)
		if combo, err := h.Repo.GetComboByName(entry); err == nil && combo != nil && combo.Models != "" {
			var subModels []string
			if err := json.Unmarshal([]byte(combo.Models), &subModels); err == nil && len(subModels) > 0 {
				first := h.resolveModelEntryGuarded(subModels[0], v)
				if first != nil {
					first.ComboModels = subModels
					strat, sticky, judge := h.resolveComboRouting(combo.Name, combo.Strategy)
					first.Strategy = strat
					first.StickyLimit = sticky
					first.JudgeModel = judge
					return first
				}
			}
		}
		return nil
	}
	parts := strings.SplitN(entry, "/", 2)
	prefix := parts[0]
	model := parts[1]

	if info := h.resolvePrefixProvider(prefix, model); info != nil {
		return info
	}

	provider := resolveProviderAlias(prefix)
	if provider != prefix {
		if info := h.resolvePrefixProvider(provider, model); info != nil {
			return info
		}
		if h.Repo != nil {
			if node, _, err := h.Repo.GetProviderNodeByPrefix(prefix); err == nil && node != nil {
				conns, _ := h.Repo.GetProviderConnections(provider, true)
				if len(conns) == 0 {
					return &ModelInfo{Provider: node.ID, Model: model}
				}
			}
		}
	}
	return &ModelInfo{Provider: provider, Model: model}
}

// flattenComboModels recursively expands combo-name entries into concrete
// "provider/model" leaves, keeping order and deduping consecutive identical
// leaves so a nested combo can't create pointless rotation slots. Guards
// against cyclic combo references by skipping recursive cycles. Inner-combo
// strategies are not applied here; the top-level combo's strategy governs
// the flattened list.
func (h *ChatHandler) flattenComboModels(models []string) ([]string, error) {
	return h.flattenComboModelsGuarded(models, newResolveVisits())
}

// flattenComboModelsGuarded is flattenComboModels with the resolution attempt's
// visited set threaded in. A name that the outer resolution is already inside is
// reported as a cycle instead of being expanded again, which is what bounds the
// alias -> combo -> alias loop: the alias rewrite makes the combo's leaf name
// the alias again, and without this the walk would re-enter it forever.
func (h *ChatHandler) flattenComboModelsGuarded(models []string, v *resolveVisits) ([]string, error) {
	out := make([]string, 0, len(models))
	seen := make(map[string]bool)
	// Gated on routing.combos for the same reason as the top-level lookup: a
	// nested combo name must not be expanded into its leaves when the operator
	// has switched combos off, or the flag would be honoured on one path and
	// ignored on another. With the flag off a nested name is left as a leaf.
	combosEnabled := h.featureFlagOn(flagRoutingCombos)
	var walk func([]string) error
	walk = func(ms []string) error {
		for _, m := range ms {
			if !strings.Contains(m, "/") {
				// A name already expanded on this walk is a pure combo cycle and
				// is skipped, keeping the previous graceful-recovery behaviour for
				// a combo that lists itself alongside a usable leaf.
				if seen[m] {
					log.Warn("combo", "cyclic combo reference detected, skipping", "combo", m)
					continue
				}
				if combosEnabled {
					if combo, err := h.Repo.GetComboByName(m); err == nil && combo != nil && combo.Models != "" {
						var sub []string
						if err := json.Unmarshal([]byte(combo.Models), &sub); err == nil {
							// A name the outer resolution is already inside is a
							// cycle across the alias/combo tables, which no
							// single-table check can see. Refuse it by name.
							if err := v.enter(m); err != nil {
								return err
							}
							seen[m] = true
							if err := walk(sub); err != nil {
								return err
							}
							delete(seen, m)
							v.leave(m)
							continue
						}
					}
				}
				if aliasTarget, err := h.Repo.GetModelAlias(m); err == nil && aliasTarget != "" && strings.Contains(aliasTarget, "/") {
					m = aliasTarget
				}
			}
			if len(out) == 0 || out[len(out)-1] != m {
				out = append(out, m)
			}
		}
		return nil
	}
	if err := walk(models); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("combo has no valid leaf models")
	}
	return out, nil
}

// stripModelContextMarker strips trailing [1m] marker that Claude Code appends for 1M context beta.
// Port of decolua/9router PR #3691 (open-sse/utils/modelMarkers.js).
// Claude Code sends model: "claude-opus-5[1m]" — the marker is client-side annotation, not a real model.
// It must be stripped before combo/alias/provider lookup, while anthropic-beta header still carries the capability.
func stripModelContextMarker(modelStr string) string {
	trimmed := strings.TrimSpace(modelStr)
	if len(trimmed) < 4 {
		return modelStr
	}
	// Case-insensitive check for trailing "[1m]"
	suffix := trimmed[len(trimmed)-4:]
	if strings.EqualFold(suffix, "[1m]") {
		// Only strip if it's a trailing marker, not bracket inside name
		return strings.TrimSpace(trimmed[:len(trimmed)-4])
	}
	return modelStr
}

// resolveModel resolves a model string through aliases, combos, and provider/model parsing.
// Returns the first concrete ModelInfo found, or an error.
func (h *ChatHandler) resolveModel(modelStr string) (*ModelInfo, error) {
	return h.resolveModelGuarded(modelStr, newResolveVisits())
}

// resolveModelGuarded is resolveModel with the resolution attempt's visited set
// threaded in, so every re-entry point on the path (the alias rewrite's
// fall-through, the combo branch's first-leaf fallback, and the nested-combo
// walk) shares one set and a name reached twice is refused with a bounded error
// naming the cycle instead of recursing until the stack is exhausted.
//
// A stack overflow here is process-fatal: recover() cannot intercept it, no
// deferred cleanup runs, and the gateway dies with every in-flight stream on it.
// The guard is therefore the difference between one request failing and the
// whole process aborting.
func (h *ChatHandler) resolveModelGuarded(modelStr string, v *resolveVisits) (*ModelInfo, error) {
	if modelStr == "" {
		return nil, fmt.Errorf("missing model")
	}
	// Strip [1m] context marker before resolution (PR #3691)
	modelStr = stripModelContextMarker(modelStr)

	// Enter this name for the duration of its resolution. The rewrite below and
	// the combo fallback both re-enter resolution with a name derived from this
	// one, so a name that comes back around is a cycle in the stored config.
	if err := v.enter(modelStr); err != nil {
		return nil, err
	}
	defer v.leave(modelStr)

	// 0. Operator model binding: the name may be bound to another model. This
	// runs before every other branch because a binding is an explicit operator
	// instruction about what the name means, and it must win over the catalog —
	// otherwise binding a name that also exists as a model would silently do
	// nothing. The binding's persona is resolved separately, at request level.
	//
	// Gated on prompt.bindings: with the flag off, bindings are skipped entirely
	// and the name falls through to alias/combo/catalog resolution below. What it
	// resolves to then depends on what else can serve the name: a bound name that
	// exists nowhere else becomes an unknown model, which is the honest off-state
	// — the same outcome the modelalias package documents for a disabled binding —
	// rather than a silent fallback to the target, which would leave the feature
	// running while the operator believed it was off. A name that the catalog or a
	// configured provider can still serve keeps resolving through those paths,
	// exactly as it would if the binding had never existed.
	if h.Repo != nil && h.featureFlagOn(flagModelBindings) {
		if binding, err := h.Repo.GetModelBinding(modelStr); err == nil && binding != nil {
			if target, ok := binding.ResolveTarget(); ok {
				log.Info("binding", "model name rewritten", "from", modelStr, "to", target, "persona", binding.Persona)
				modelStr = target
			}
		}
	}

	// 1. Standard format: "provider/model"
	if strings.Contains(modelStr, "/") {
		parts := strings.SplitN(modelStr, "/", 2)
		prefix := parts[0]
		model := parts[1]

		// Check custom prefix provider node first (before built-in alias resolution shadows it, e.g. "oa" or "cc")
		if info := h.resolvePrefixProvider(prefix, model); info != nil {
			return info, nil
		}

		provider := resolveProviderAlias(prefix)
		if provider != prefix {
			if info := h.resolvePrefixProvider(provider, model); info != nil {
				return info, nil
			}
			// If the alias-resolved provider has no active connections, check if the prefix
			// matches a providerNode so errors point to the intended custom node ID.
			if h.Repo != nil {
				if node, _, err := h.Repo.GetProviderNodeByPrefix(prefix); err == nil && node != nil {
					conns, _ := h.Repo.GetProviderConnections(provider, true)
					if len(conns) == 0 {
						return &ModelInfo{Provider: node.ID, Model: model}, nil
					}
				}
			}
		}
		return &ModelInfo{Provider: provider, Model: model}, nil
	}

	// 2. Check if it's a model alias (e.g., "gpt-4o" -> "openai/gpt-4o")
	if h.Repo != nil {
		aliasTarget, err := h.Repo.GetModelAlias(modelStr)
		if err == nil && aliasTarget != "" {
			if strings.Contains(aliasTarget, "/") {
				parts := strings.SplitN(aliasTarget, "/", 2)
				prefix := parts[0]
				model := parts[1]

				if info := h.resolvePrefixProvider(prefix, model); info != nil {
					return info, nil
				}

				provider := resolveProviderAlias(prefix)
				if provider != prefix {
					if info := h.resolvePrefixProvider(provider, model); info != nil {
						return info, nil
					}
					if h.Repo != nil {
						if node, _, err := h.Repo.GetProviderNodeByPrefix(prefix); err == nil && node != nil {
							conns, _ := h.Repo.GetProviderConnections(provider, true)
							if len(conns) == 0 {
								return &ModelInfo{Provider: node.ID, Model: model}, nil
							}
						}
					}
				}
				return &ModelInfo{
					Provider: provider,
					Model:    model,
				}, nil
			}
			// Alias target without a provider prefix (e.g. "shared" ->
			// "deepseek-chat"). The alias is still the operator's most specific
			// rename instruction, so it must win over a same-named combo —
			// including that branch's fail-loud below, which would otherwise
			// reject a name this alias can serve whenever routing.combos is off.
			// Resolution continues with the rewritten name, so the target is
			// resolved exactly as if the client had sent it.
			//
			// The rewritten name is entered into the visited set for the same
			// reason every other re-entry point is: the target is resolved by
			// falling through to the combo branch below, so if a combo reachable
			// from it names this alias again the resolution would otherwise loop
			// without bound. Entering it makes the loop visible as a cycle whose
			// chain names both the alias and the combo.
			if aliasTarget != modelStr {
				if err := v.enter(aliasTarget); err != nil {
					return nil, err
				}
				defer v.leave(aliasTarget)
				modelStr = aliasTarget
			}
		}
	}

	// Upstream PR #4135: route bare codex-auto-review to the Codex provider
	// Outside Repo guard so it resolves with nil Repo / empty DB (static catalog).
	if modelStr == "codex-auto-review" {
		return &ModelInfo{Provider: "codex", Model: "codex-auto-review"}, nil
	}

	// 3. Check if it's a combo name.
	//
	// Gated on routing.combos. A combo name must never be silently resolved to
	// something else: the name is an operator-authored routing plan (rotation,
	// fallback order, strategy, sticky limit, judge model), and the catalog
	// fallback below would fabricate a single model out of it — dropping all of
	// that while reporting success. So the flag has two outcomes here:
	//
	//   - on: the combo is expanded and its routing plan applies;
	//   - off: a name that IS a stored combo fails loudly, because the operator
	//     switched off the feature that gives the name its meaning. Resolving it
	//     as a fabricated model would serve one arbitrary leaf while the operator
	//     believed the combo was inert.
	//
	// A name that is not a combo is untouched by this and keeps resolving through
	// the alias/catalog branches below exactly as before, which is the honest
	// off-state for everything else.
	if h.Repo != nil {
		combo, comboErr := h.Repo.GetComboByName(modelStr)
		if comboErr != nil {
			// A storage failure leaves this name's meaning unknown. It is logged
			// rather than resolved either way: failing hard would turn an
			// unrelated storage problem into an outage of every bare-name
			// request, and the alias/catalog branches below are the same
			// resolution this name would have had before the combo table was
			// consulted at all.
			log.Warn("combo", "combo lookup failed", "model", modelStr, "error", comboErr)
		} else if combo != nil && combo.Models != "" {
			if !h.featureFlagOn(flagRoutingCombos) {
				return nil, fmt.Errorf("model %q is a combo but combos are disabled (feature flag routing.combos is off)", modelStr)
			}
			var modelStrings []string
			if err := json.Unmarshal([]byte(combo.Models), &modelStrings); err == nil && len(modelStrings) > 0 {
				// Flatten nested combos into concrete leaves so rotation covers
				// every reachable model (a nested combo entry used to collapse to
				// its first leaf, so combo-wombo -> free-tier never rotated).
				flattened, flatErr := h.flattenComboModelsGuarded(modelStrings, v)
				if flatErr != nil {
					return nil, flatErr
				}
				if len(flattened) > 0 {
					firstInfo := h.resolveModelEntryGuarded(flattened[0], v)
					if firstInfo == nil {
						// The first leaf could not be parsed as an entry. Fall
						// back to full resolution so a leaf that is itself an
						// alias or bare provider still resolves. A cycle error
						// is propagated rather than swallowed: it is the bounded,
						// actionable report of a looping stored config, and
						// dropping it would replace a named cycle with a generic
						// "could not resolve model" further down.
						fallbackInfo, fallbackErr := h.resolveModelGuarded(flattened[0], v)
						if fallbackErr != nil && errors.Is(fallbackErr, errModelResolutionCycle) {
							return nil, fallbackErr
						}
						firstInfo = fallbackInfo
					}
					if firstInfo != nil {
						firstInfo.ComboModels = flattened
						strat, sticky, judge := h.resolveComboRouting(combo.Name, combo.Strategy)
						firstInfo.Strategy = strat
						firstInfo.StickyLimit = sticky
						firstInfo.JudgeModel = judge
						return firstInfo, nil
					}
				}
			}
		}
	}

	// 3.5 Check if it's a bare provider alias (e.g., "ag" -> "antigravity")
	// Check custom prefix provider nodes first for bare alias
	if info := h.resolvePrefixProvider(modelStr, ""); info != nil {
		return info, nil
	}
	if h.Repo != nil {
		if canonical := resolveProviderAlias(modelStr); canonical != modelStr {
			if _, ok := providers.KnownProviders[canonical]; ok {
				if conns, err := h.Repo.GetProviderConnections(canonical, true); err == nil && len(conns) > 0 {
					return &ModelInfo{Provider: canonical, Model: ""}, nil
				}
			}
		}
		if _, ok := providers.KnownProviders[modelStr]; ok {
			if conns, err := h.Repo.GetProviderConnections(modelStr, true); err == nil && len(conns) > 0 {
				return &ModelInfo{Provider: modelStr, Model: ""}, nil
			}
		}

		// 4. Check common providers as a fallback
		for _, provider := range []string{"openai", "anthropic", "deepseek"} {
			conns, err := h.Repo.GetProviderConnections(provider, true)
			if err == nil && len(conns) > 0 {
				return &ModelInfo{Provider: provider, Model: modelStr}, nil
			}
		}
	}
	return nil, fmt.Errorf("could not resolve model: %s", modelStr)
}

// resolvePrefixProvider checks if a provider name is a providerNode prefix.
// If so, it finds the matching connection and returns a pinned ModelInfo.
func (h *ChatHandler) resolvePrefixProvider(prefix string, model string) *ModelInfo {
	if h.Repo == nil {
		return nil
	}
	node, _, err := h.Repo.GetProviderNodeByPrefix(prefix)
	if err != nil || node == nil {
		return nil
	}

	conn, _, err := h.getBestConnection(node.ID, "", nil, model)
	if err != nil || conn == nil {
		return nil
	}

	return &ModelInfo{
		Provider:     node.ID,
		Model:        model,
		ConnectionID: conn.ID,
	}
}

// resolveComboRouting retrieves the routing strategy, sticky limit, and judge model
// for a combo, prioritizing per-combo settings, then global combo settings, then combo.Strategy.
func (h *ChatHandler) resolveComboRouting(comboName string, fallbackStrategy string) (strategy string, stickyLimit int, judgeModel string) {
	strategy = fallbackStrategy
	if strategy == "" {
		strategy = "fallback"
	}
	stickyLimit = 1

	if h.Repo == nil {
		return strategy, stickyLimit, ""
	}

	settings, err := h.Repo.GetSettings()
	if err != nil || settings == nil {
		return strategy, stickyLimit, ""
	}

	if cs, ok := settings.ComboStrategies[comboName]; ok {
		if cs.Strategy != "" {
			strategy = cs.Strategy
		}
		if cs.StickyLimit > 0 {
			stickyLimit = cs.StickyLimit
		}
		judgeModel = cs.JudgeModel
		return strategy, stickyLimit, judgeModel
	}

	if settings.ComboStrategy != "" {
		strategy = settings.ComboStrategy
	}
	if settings.ComboStickyRoundRobinLimit > 0 {
		stickyLimit = settings.ComboStickyRoundRobinLimit
	}

	return strategy, stickyLimit, ""
}
