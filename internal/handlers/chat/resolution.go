package chat

import (
	json "encoding/json/v2"
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
	if !strings.Contains(entry, "/") {
		if h.Repo == nil || !h.featureFlagOn(flagRoutingCombos) {
			return nil
		}
		if combo, err := h.Repo.GetComboByName(entry); err == nil && combo != nil && combo.Models != "" {
			var subModels []string
			if err := json.Unmarshal([]byte(combo.Models), &subModels); err == nil && len(subModels) > 0 {
				first := h.resolveModelEntry(subModels[0])
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
				if seen[m] {
					log.Warn("combo", "cyclic combo reference detected, skipping", "combo", m)
					continue
				}
				if combosEnabled {
					if combo, err := h.Repo.GetComboByName(m); err == nil && combo != nil && combo.Models != "" {
						var sub []string
						if err := json.Unmarshal([]byte(combo.Models), &sub); err == nil {
							seen[m] = true
							if err := walk(sub); err != nil {
								return err
							}
							delete(seen, m)
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
	if modelStr == "" {
		return nil, fmt.Errorf("missing model")
	}
	// Strip [1m] context marker before resolution (PR #3691)
	modelStr = stripModelContextMarker(modelStr)

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
		}
	}

	// Upstream PR #4135: route bare codex-auto-review to the Codex provider
	// Outside Repo guard so it resolves with nil Repo / empty DB (static catalog).
	if modelStr == "codex-auto-review" {
		return &ModelInfo{Provider: "codex", Model: "codex-auto-review"}, nil
	}

	// 3. Check if it's a combo name.
	//
	// Gated on routing.combos: with the flag off, a combo name is never expanded
	// here and falls through to the alias/catalog branches below, resolving
	// exactly as if the combo table were empty. A name that exists only as a
	// combo then becomes an unknown model, which is the honest off-state rather
	// than a silent expansion of a feature the operator switched off.
	if h.Repo != nil && h.featureFlagOn(flagRoutingCombos) {
		combo, err := h.Repo.GetComboByName(modelStr)
		if err == nil && combo != nil && combo.Models != "" {
			var modelStrings []string
			if err := json.Unmarshal([]byte(combo.Models), &modelStrings); err == nil && len(modelStrings) > 0 {
				// Flatten nested combos into concrete leaves so rotation covers
				// every reachable model (a nested combo entry used to collapse to
				// its first leaf, so combo-wombo -> free-tier never rotated).
				flattened, flatErr := h.flattenComboModels(modelStrings)
				if flatErr != nil {
					return nil, flatErr
				}
				if len(flattened) > 0 {
					firstInfo := h.resolveModelEntry(flattened[0])
					if firstInfo == nil {
						firstInfo, _ = h.resolveModel(flattened[0])
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
