package modelgraph

import (
	"fmt"
	"testing"
	"time"
)

// The validator runs on every alias/combo write, so a pathological graph must
// not make the dashboard unresponsive. This measures the worst realistic case:
// a long chain plus a wide fan-out, validated from every seed.
func TestValidatePerformanceOnLargeGraph(t *testing.T) {
	combos := map[string][]string{}
	for i := 0; i < 2000; i++ {
		combos[fmt.Sprintf("c%d", i)] = []string{fmt.Sprintf("c%d", i+1)}
	}
	combos["c2000"] = []string{"deepseek/deepseek-chat"}
	wide := make([]string, 1000)
	for i := range wide {
		wide[i] = fmt.Sprintf("c%d", i)
	}
	combos["wide"] = wide
	aliases := map[string]string{}
	for i := 0; i < 500; i++ {
		aliases[fmt.Sprintf("a%d", i)] = fmt.Sprintf("c%d", i)
	}
	g := NewGraph(aliases, combos)

	start := time.Now()
	if c := g.Validate(); c != nil {
		t.Fatalf("acyclic graph reported as loop: %s", Describe(c))
	}
	elapsed := time.Since(start)
	t.Logf("whole-graph validation of %d names took %s", len(aliases)+len(combos), elapsed)
	if elapsed > 5*time.Second {
		t.Errorf("validation too slow for a write path: %s", elapsed)
	}
}
