package modelgraph

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// Hostile and degenerate inputs must not panic, hang, or report a false loop.
func TestValidateSurvivesHostileInput(t *testing.T) {
	cases := []struct {
		name string
		g    Graph
	}{
		{"nil maps", Graph{}},
		{"empty strings", NewGraph(map[string]string{"": ""}, map[string][]string{"": {"", ""}})},
		{"whitespace names", NewGraph(map[string]string{"  ": "  "}, nil)},
		{"unicode names", NewGraph(map[string]string{"日本語": "中文"}, map[string][]string{"中文": {"日本語"}})},
		{"very long name", NewGraph(map[string]string{strings.Repeat("x", 5000): "y"}, nil)},
		{"huge leaf list", NewGraph(nil, map[string][]string{"big": make([]string, 5000)})},
		{"nil leaf slice", NewGraph(nil, map[string][]string{"n": nil})},
		{"dense fan-out", func() Graph {
			leaves := make([]string, 500)
			for i := range leaves {
				leaves[i] = name(i)
			}
			return NewGraph(nil, map[string][]string{"root": leaves})
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan *Cycle, 1)
			go func() { done <- tc.g.Validate() }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("Validate did not return: possible hang")
			}
		})
	}
}

// Concurrent validation must be safe: the graph is read-only and the walk state
// is per-call, so parallel requests cannot corrupt each other or see a false
// cycle from another goroutine's in-flight walk.
func TestValidateIsConcurrencySafe(t *testing.T) {
	combos := map[string][]string{}
	for i := 0; i < 50; i++ {
		combos[name(i)] = []string{name(i + 1)}
	}
	combos[name(50)] = []string{"deepseek/deepseek-chat"}
	g := NewGraph(map[string]string{"fast": name(0)}, combos)

	var wg sync.WaitGroup
	errs := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c := g.Validate(); c != nil {
				errs <- "false cycle: " + Describe(c)
			}
			if c := g.ValidateAliasWrite("fast", name(0)); c != nil {
				errs <- "false cycle on alias write: " + Describe(c)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
