package providers

import (
	"strings"
	"testing"
)

// The audit names the rule that produced a limit, so its rule list MUST agree
// with GetModelTokenLimits. A silent drift would make the audit report a
// published figure for a number the gateway actually guessed — the exact failure
// the tool exists to prevent.
func TestExplainLimitsMatchesResolver(t *testing.T) {
	models := []string{
		"deepseek-v4-flash",
		"deepseek-v4.1-flash-vision-exp",
		"gemini-3.8-flash-high",
		"glm-5.3-flash",
		"grok-4.5",
		"gpt-6",
		"claude-sonnet-4-6",
		"gpt-5.4",
		"solar-pro",
		"o3",
		"qwen3.5-plus",
		"kimi-k2.6",
		"mimo-v2.5-pro",
		"minimax-m3",
		"some-unknown-model-xyz",
	}
	for _, model := range models {
		wantContext, wantOutput := GetModelTokenLimits(model)
		got := ExplainModelTokenLimits(model)
		if got.ContextWindow != wantContext || got.MaxOutput != wantOutput {
			t.Errorf("%s: audit says %d/%d, resolver says %d/%d",
				model, got.ContextWindow, got.MaxOutput, wantContext, wantOutput)
		}
	}
}

// A model no rule matches must be reported as a guess, not as a known figure.
func TestExplainLimitsReportsFallbackAsGuess(t *testing.T) {
	got := ExplainModelTokenLimits("totally-unheard-of-model")
	if got.Source != LimitSourceUnknownModel {
		t.Fatalf("fallback source = %q, want %q", got.Source, LimitSourceUnknownModel)
	}
	if got.MatchedRule != "" {
		t.Errorf("fallback should name no rule, got %q", got.MatchedRule)
	}
	if got.ContextWindow != FallbackContextWindow {
		t.Errorf("fallback context = %d, want %d", got.ContextWindow, FallbackContextWindow)
	}
}

// A rule match must claim a rule, so the two outcomes stay distinguishable.
func TestExplainLimitsReportsKnownRule(t *testing.T) {
	got := ExplainModelTokenLimits("claude-opus-4-6-thinking")
	if got.Source != LimitSourceKnown || got.MatchedRule == "" {
		t.Fatalf("expected a named rule, got %+v", got)
	}
}

// The audit must cover the whole catalog and partition it exactly: every guessed
// entry is in the full list and vice versa is never double counted.
func TestAuditLimitsPartitionsCatalog(t *testing.T) {
	all, guessed := AuditLimits()
	if len(all) != len(CatalogModels()) {
		t.Fatalf("audit covered %d entries, catalog has %d", len(all), len(CatalogModels()))
	}
	if len(guessed) > len(all) {
		t.Fatalf("guessed (%d) exceeds total (%d)", len(guessed), len(all))
	}
	guessedModels := make(map[string]bool, len(guessed))
	for _, limit := range guessed {
		if limit.Source == LimitSourceKnown {
			t.Errorf("%s/%s is in the guessed set but claims a rule", limit.Provider, limit.Model)
		}
		if limit.MatchedRule != "" {
			t.Errorf("%s/%s guessed entry names rule %q", limit.Provider, limit.Model, limit.MatchedRule)
		}
		guessedModels[limit.Provider+"|"+limit.Model] = true
	}
	for _, limit := range all {
		inGuessed := guessedModels[limit.Provider+"|"+limit.Model]
		if inGuessed != (limit.Source != LimitSourceKnown) {
			t.Errorf("%s/%s misclassified: source=%q guessed=%v", limit.Provider, limit.Model, limit.Source, inGuessed)
		}
	}
}

// The summary must state the guess count so a reader cannot mistake the audit for
// a clean bill of health.
func TestSummarizeLimitsStatesGuesses(t *testing.T) {
	all, guessed := AuditLimits()
	lines := SummarizeLimits(all, guessed)
	joined := ""
	for _, line := range lines {
		joined += line + "\n"
	}
	if len(guessed) > 0 && !strings.Contains(joined, "Fallback guesses") {
		t.Errorf("summary hides the guess count:\n%s", joined)
	}
	if !strings.Contains(joined, "Catalog models:") {
		t.Errorf("summary omits the total:\n%s", joined)
	}
}
