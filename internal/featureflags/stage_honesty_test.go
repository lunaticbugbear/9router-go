package featureflags_test

import (
	"sort"
	"testing"

	"9router/proxy/internal/featureflags"
	"9router/proxy/internal/handlers/chat"
)

// TestStableFlagsAreWired is the enforcement behind the Stage type.
//
// Stage exists to keep the registry honest: Stable means "this toggle controls
// live behavior today". A flag labeled Stable that gates nothing is exactly the
// lie the label was introduced to prevent, and it is invisible in review — the
// flag looks wired because it is listed, and the toggle appears to work because
// nothing errors. So the set of Stable flags is cross-checked against the set of
// flags the request plane actually gates on.
//
// Adding a Stable flag therefore means wiring it and adding its id to
// chat.WiredFlagIDs. A flag that is not wired yet must be Planned, which is
// what Planned is for.
//
// This test lives in an external test package because it must import both the
// registry and the package that consumes it; importing chat from inside package
// featureflags would be an import cycle.
func TestStableFlagsAreWired(t *testing.T) {
	var stable []string
	for _, f := range featureflags.All() {
		if f.Stage == featureflags.Stable {
			stable = append(stable, f.ID)
		}
	}
	sort.Strings(stable)

	wired := make([]string, 0, len(chat.WiredFlagIDs))
	seen := make(map[string]bool, len(chat.WiredFlagIDs))
	for _, id := range chat.WiredFlagIDs {
		if seen[id] {
			t.Errorf("chat.WiredFlagIDs lists %q twice", id)
			continue
		}
		seen[id] = true
		wired = append(wired, id)
	}
	sort.Strings(wired)

	for _, id := range wired {
		if _, ok := featureflags.Get(id); !ok {
			t.Errorf("chat.WiredFlagIDs lists %q, which is not in the registry", id)
		}
	}

	if len(stable) != len(wired) {
		t.Fatalf("Stable flags and wired flags disagree: %d stable %v, %d wired %v",
			len(stable), stable, len(wired), wired)
	}
	for i := range stable {
		if stable[i] != wired[i] {
			t.Fatalf("Stable flags and wired flags disagree:\n stable %v\n  wired %v", stable, wired)
		}
	}

	// A wired flag must be Stable: gating live behavior on a Planned flag would
	// mean the registry tells the operator the toggle does nothing while it in
	// fact changes traffic.
	for _, id := range wired {
		f, _ := featureflags.Get(id)
		if f.Stage != featureflags.Stable {
			t.Errorf("%q is wired but not Stable (stage=%s)", id, f.Stage)
		}
	}
}
