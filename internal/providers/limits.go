package providers

import (
	"fmt"
	"sort"
	"strings"
)

// LimitSource names where a model's context window actually came from. The
// distinction matters because the resolver's last resort is a guess: without it
// a caller cannot tell a real limit from a placeholder, which is how a
// dashboard ends up advertising a number no provider ever published.
type LimitSource string

const (
	// LimitSourceKnown means a rule matched the model name explicitly.
	LimitSourceKnown LimitSource = "known"
	// LimitSourceUnknownModel means no rule matched and the fallback was used.
	LimitSourceUnknownModel LimitSource = "unknown-model"
	// LimitSourceUnknownProvider means the model could not be attributed to a
	// provider in the catalog.
	LimitSourceUnknownProvider LimitSource = "unknown-provider"
)

// ModelLimit explains one resolved context window.
type ModelLimit struct {
	Model         string      `json:"model"`
	Provider      string      `json:"provider,omitempty"`
	ContextWindow int         `json:"contextWindow"`
	MaxOutput     int         `json:"maxOutput"`
	Source        LimitSource `json:"source"`
	// MatchedRule is the friendly name of the rule that produced the value, or
	// empty when the fallback was used.
	MatchedRule string `json:"matchedRule,omitempty"`
}

// limitRule pairs a substring predicate with the values it asserts and a name
// for reporting.
type limitRule struct {
	name          string
	contextWindow int
	maxOutput     int
	matches       func(string) bool
}

// limitRules mirrors GetModelTokenLimits exactly, in the same order, so the
// audit can name the rule that produced a value. Keeping both in this file means
// a rule change cannot silently desynchronise the number from its explanation.
var limitRules = []limitRule{
	{"deepseek-v4-flash", 1000000, 128000, func(m string) bool {
		return strings.Contains(m, "deepseek-v4.1-flash") || strings.Contains(m, "deepseek-v4-flash")
	}},
	{"gemini-flash-family", 1048576, 65536, func(m string) bool {
		return strings.Contains(m, "gemini-1.5") || strings.Contains(m, "gemini-2.0") ||
			strings.Contains(m, "gemini-2.5") || strings.Contains(m, "gemini-3") ||
			strings.Contains(m, "glm-5.3-flash")
	}},
	{"grok-4.x", 524288, 32768, func(m string) bool {
		return strings.Contains(m, "grok-4.5") || strings.Contains(m, "grok-4.6")
	}},
	{"gpt-6", 272000, 128000, func(m string) bool { return strings.Contains(m, "gpt-6") }},
	{"claude-family", 200000, 8192, func(m string) bool {
		return strings.Contains(m, "claude-3") || strings.Contains(m, "claude-sonnet") ||
			strings.Contains(m, "claude-opus") || strings.Contains(m, "claude-haiku")
	}},
	{"gpt-4/5-family", 128000, 16384, func(m string) bool {
		return strings.Contains(m, "gpt-4o") || strings.Contains(m, "gpt-4-turbo") ||
			strings.Contains(m, "gpt-4.1") || strings.Contains(m, "gpt-5")
	}},
	{"solar-longcat", 200000, 32000, func(m string) bool {
		return strings.Contains(m, "solar-pro") || strings.Contains(m, "longcat")
	}},
	{"o-series", 200000, 100000, func(m string) bool {
		return strings.Contains(m, "o1") || strings.Contains(m, "o3")
	}},
	{"deepseek-qwen-glm-kimi", 131072, 8192, func(m string) bool {
		return strings.Contains(m, "deepseek") || strings.Contains(m, "qwen") ||
			strings.Contains(m, "glm") || strings.Contains(m, "kimi")
	}},
}

// FallbackContextWindow and FallbackMaxOutput are the values used when no rule
// matches. They are guesses, and the audit reports them as such.
const (
	FallbackContextWindow = 128000
	FallbackMaxOutput     = 4096
)

// ExplainModelTokenLimits resolves a limit and reports which rule produced it,
// so a caller can distinguish a published number from a fallback guess.
func ExplainModelTokenLimits(model string) ModelLimit {
	m := strings.ToLower(model)
	for _, rule := range limitRules {
		if rule.matches(m) {
			return ModelLimit{
				Model:         model,
				ContextWindow: rule.contextWindow,
				MaxOutput:     rule.maxOutput,
				Source:        LimitSourceKnown,
				MatchedRule:   rule.name,
			}
		}
	}
	return ModelLimit{
		Model:         model,
		ContextWindow: FallbackContextWindow,
		MaxOutput:     FallbackMaxOutput,
		Source:        LimitSourceUnknownModel,
	}
}

// CatalogEntry is one model as the gateway advertises it.
type CatalogEntry struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// CatalogModels flattens ProviderModels into a stable, sorted list. Models
// reachable under several providers keep one entry each: a provider that
// overrides a name is a distinct claim, not a duplicate.
func CatalogModels() []CatalogEntry {
	entries := make([]CatalogEntry, 0, 512)
	for provider, models := range ProviderModels {
		for _, model := range models {
			entries = append(entries, CatalogEntry{Provider: provider, Model: model})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Provider != entries[j].Provider {
			return entries[i].Provider < entries[j].Provider
		}
		return entries[i].Model < entries[j].Model
	})
	return entries
}

// AuditLimits explains the limits for every catalog model and returns the full
// list plus the subset that fell back to a guess. A short model list is not an
// error: the caller decides what an acceptable ratio is.
func AuditLimits() (all []ModelLimit, guessed []ModelLimit) {
	for _, entry := range CatalogModels() {
		limit := ExplainModelTokenLimits(entry.Model)
		limit.Provider = entry.Provider
		all = append(all, limit)
		if limit.Source != LimitSourceKnown {
			guessed = append(guessed, limit)
		}
	}
	return all, guessed
}

// SummarizeLimits renders the audit as the operator-facing lines used by the
// `models audit` command: totals first, then the guessed entries.
func SummarizeLimits(all []ModelLimit, guessed []ModelLimit) []string {
	lines := []string{
		fmt.Sprintf("Catalog models: %d", len(all)),
		fmt.Sprintf("Limits from a matching rule: %d", len(all)-len(guessed)),
	}
	if len(guessed) == 0 {
		lines = append(lines, "Fallback guesses: 0")
		return lines
	}
	lines = append(lines,
		fmt.Sprintf("Fallback guesses: %d (%.1f%%)", len(guessed), 100*float64(len(guessed))/float64(len(all))),
		"",
		fmt.Sprintf("These models were reported as %d tokens by the fallback, not by a published figure:",
			FallbackContextWindow),
	)
	for _, limit := range guessed {
		lines = append(lines, fmt.Sprintf("  %s/%s", limit.Provider, limit.Model))
	}
	return lines
}
