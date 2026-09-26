package featureflags

import (
	"strings"
	"testing"
)

// Every flag must carry the fields the dashboard renders. A missing title or
// description shows up as a blank row the operator cannot decide about, so the
// registry rejects them here rather than at render time.
func TestEveryFlagIsDescribed(t *testing.T) {
	seen := make(map[string]bool)
	for _, f := range All() {
		if f.ID == "" {
			t.Fatal("flag with empty ID")
		}
		if seen[f.ID] {
			t.Errorf("duplicate flag id %q", f.ID)
		}
		seen[f.ID] = true
		if strings.TrimSpace(f.Title) == "" {
			t.Errorf("%s: missing title", f.ID)
		}
		if len(strings.TrimSpace(f.Description)) < 20 {
			t.Errorf("%s: description too short to decide from: %q", f.ID, f.Description)
		}
		if strings.TrimSpace(f.Category) == "" {
			t.Errorf("%s: missing category", f.ID)
		}
	}
}

// A default-on flag must not be a behavior that costs money, changes outbound
// bodies, or stores more data. This is the rule that keeps a fresh install
// conservative; breaking it changes what a new user's first requests look like.
func TestDefaultOnFlagsAreConservative(t *testing.T) {
	for _, f := range All() {
		if !f.Default {
			continue
		}
		if f.Stage == Planned {
			t.Errorf("%s: planned flags must default to off (a planned toggle does nothing)", f.ID)
		}
		for _, costWord := range []string{"store full", "Encrypt", "aggressive"} {
			if strings.Contains(f.Description, costWord) {
				t.Errorf("%s: default-on flag describes costly behavior %q", f.ID, costWord)
			}
		}
	}
}

// Get must refuse unknown ids rather than invent a default: an unknown flag is
// a bug in a caller, and guessing hides it.
func TestGetRejectsUnknownIDs(t *testing.T) {
	if _, ok := Get("no.such.flag"); ok {
		t.Error("Get returned a flag for an unknown id")
	}
	if _, ok := Get("prompt.personas"); !ok {
		t.Error("Get failed for a registered flag")
	}
}

// Categories must come out in registry order and without duplicates, because
// the dashboard renders sections straight from it.
func TestCategoriesOrderedAndUnique(t *testing.T) {
	cats := Categories()
	seen := make(map[string]bool)
	last := -1
	for _, c := range cats {
		if seen[c] {
			t.Errorf("duplicate category %q", c)
		}
		seen[c] = true
		found := -1
		for i, f := range All() {
			if f.Category == c {
				found = i
				break
			}
		}
		if found < last {
			t.Errorf("category %q rendered out of registry order", c)
		}
		last = found
	}
}

// The stable set is what a fresh install actually runs with. If this changes,
// a default behavior changed — that deserves a deliberate look, so the count
// is pinned.
func TestStableFlagSetIsPinned(t *testing.T) {
	var stableIDs []string
	for _, f := range All() {
		if f.Stage == Stable {
			stableIDs = append(stableIDs, f.ID)
		}
	}
	want := []string{
		"routing.combos",
		"routing.sticky-sessions",
		"tokensavers.rtk",
		"observation.session-tracing",
		"observation.model-audit",
		"prompt.personas",
		"prompt.bindings",
		"prompt.bounty",
	}
	if len(stableIDs) != len(want) {
		t.Fatalf("stable flags changed: got %d %v", len(stableIDs), stableIDs)
	}
	for i := range want {
		if stableIDs[i] != want[i] {
			t.Fatalf("stable flags changed: got %v want %v", stableIDs, want)
		}
	}
}
