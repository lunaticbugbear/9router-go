package modelgraph

import (
	"strings"
	"testing"
)

// The exact shape that caused the process-fatal stack overflow: alias "cyc"
// points at combo "cyc-combo", whose leaf list points back at "cyc". A check
// over either store alone cannot see this, because the two halves live in
// different tables.
func TestValidateCatchesCrossStoreLoop(t *testing.T) {
	g := NewGraph(
		map[string]string{"cyc": "cyc-combo"},
		map[string][]string{"cyc-combo": {"cyc"}},
	)
	c := g.Validate()
	if c == nil {
		t.Fatal("cross-store loop was not detected")
	}
	joined := strings.Join(c.Path, " -> ")
	if !strings.Contains(joined, "cyc") || !strings.Contains(joined, "cyc-combo") {
		t.Errorf("cycle report does not name both names: %q", joined)
	}
	if c.Path[0] != c.Path[len(c.Path)-1] {
		t.Errorf("cycle path must start and end at the same name: %v", c.Path)
	}
	if !strings.Contains(Describe(c), "model resolution loop") {
		t.Errorf("operator message does not describe a loop: %q", Describe(c))
	}
}

// A proposed alias write must be refused BEFORE it is stored, and the refusal
// must name the loop so the operator can fix it.
func TestValidateAliasWriteRefusesLoopBeforeStoring(t *testing.T) {
	// Current state is clean; the proposed write closes the loop.
	g := NewGraph(nil, map[string][]string{"cyc-combo": {"cyc"}})
	if c := g.Validate(); c != nil {
		t.Fatalf("graph should start clean, got %v", c.Path)
	}
	c := g.ValidateAliasWrite("cyc", "cyc-combo")
	if c == nil {
		t.Fatal("alias write that closes a loop must be refused")
	}
	if !strings.Contains(Describe(c), "alias") {
		t.Errorf("refusal should tell the operator which store to edit: %q", Describe(c))
	}
}

// Same for a combo write: the combo that closes the loop is refused.
func TestValidateComboWriteRefusesLoopBeforeStoring(t *testing.T) {
	g := NewGraph(map[string]string{"cyc": "cyc-combo"}, nil)
	if c := g.ValidateComboWrite("cyc-combo", []string{"cyc"}); c == nil {
		t.Fatal("combo write that closes a loop must be refused")
	}
	// A combo that does not close a loop is accepted.
	if c := g.ValidateComboWrite("cyc-combo", []string{"deepseek/deepseek-chat"}); c != nil {
		t.Fatalf("harmless combo write was refused: %v", c.Path)
	}
}

// A diamond is NOT a loop: the same leaf twice in one combo, and two combos
// sharing a leaf, are legitimate configs. Reporting them would break working
// setups — a false positive here is as damaging as the original crash.
func TestValidateAcceptsDiamonds(t *testing.T) {
	cases := []struct {
		name string
		g    Graph
	}{
		{
			name: "same leaf twice in one combo",
			g:    NewGraph(nil, map[string][]string{"d": {"shared", "shared"}}),
		},
		{
			name: "two combos sharing a leaf",
			g: NewGraph(nil, map[string][]string{
				"a": {"shared"},
				"b": {"shared"},
			}),
		},
		{
			name: "nested combos sharing a leaf",
			g: NewGraph(nil, map[string][]string{
				"outer":   {"inner-a", "inner-b"},
				"inner-a": {"shared"},
				"inner-b": {"shared"},
			}),
		},
		{
			name: "alias pointing at a combo that is not a loop",
			g:    NewGraph(map[string]string{"fast": "team"}, map[string][]string{"team": {"deepseek/deepseek-chat"}}),
		},
	}
	for _, tc := range cases {
		if c := tc.g.Validate(); c != nil {
			t.Errorf("%s: reported a loop where none exists: %s", tc.name, Describe(c))
		}
	}
}

// A name in both stores is validated on both edges: the alias wins at
// resolution, but the combo edge is still reachable, so ignoring it would let a
// real loop through.
func TestValidateConsidersBothStoresForOneName(t *testing.T) {
	g := NewGraph(
		map[string]string{"both": "deepseek/deepseek-chat"},
		map[string][]string{"both": {"looper"}, "looper": {"both"}},
	)
	if c := g.Validate(); c == nil {
		t.Fatal("loop reachable only through the combo edge of a dual-store name was missed")
	}
}

// Removing a name must clear its edges, so an update is validated against the
// state the write leaves behind rather than the state it replaces.
func TestWithoutRemovesEdges(t *testing.T) {
	g := NewGraph(map[string]string{"a": "b"}, map[string][]string{"b": {"a"}})
	if g.Validate() == nil {
		t.Fatal("loop should exist before removal")
	}
	if c := g.WithoutCombo("b").Validate(); c != nil {
		t.Fatalf("loop persisted after removing the combo edge: %v", c.Path)
	}
	if c := g.WithoutAlias("a").Validate(); c != nil {
		t.Fatalf("loop persisted after removing the alias edge: %v", c.Path)
	}
}

// Self-reference is a loop of one and must be caught on both edge kinds.
func TestValidateCatchesSelfReference(t *testing.T) {
	if c := NewGraph(nil, map[string][]string{"s": {"s"}}).Validate(); c == nil {
		t.Error("self-referential combo not detected")
	}
	// A self-alias is inert at runtime (resolution guards it), and the graph
	// walk skips it rather than reporting a loop the operator cannot act on.
	if c := NewGraph(map[string]string{"s": "s"}, nil).Validate(); c != nil {
		t.Errorf("self-alias reported as a loop: %v", c.Path)
	}
}

// Validation must not mutate the graph it was given, so a refused write leaves
// the live config untouched.
func TestValidateDoesNotMutate(t *testing.T) {
	aliases := map[string]string{"a": "b"}
	combos := map[string][]string{"b": {"deepseek/deepseek-chat"}}
	g := NewGraph(aliases, combos)
	_ = g.ValidateAliasWrite("a", "b")
	_ = g.WithCombo("b", []string{"a"})
	if aliases["a"] != "b" || len(combos["b"]) != 1 || combos["b"][0] != "deepseek/deepseek-chat" {
		t.Fatal("validation mutated the caller's maps")
	}
	if g.Aliases["a"] != "b" || g.Combos["b"][0] != "deepseek/deepseek-chat" {
		t.Fatal("validation mutated the graph")
	}
}

// A combo that names itself among concrete leaves works today (the runtime walk
// skips the cycle and keeps the concrete leaf), so refusing it would break a
// working config. Only a pure self-reference is refused.
func TestValidateDistinguishesMixedSelfReference(t *testing.T) {
	mixed := NewGraph(nil, map[string][]string{"s": {"s", "deepseek/deepseek-chat"}})
	if c := mixed.Validate(); c != nil {
		t.Errorf("mixed self-reference wrongly refused: %s", Describe(c))
	}
	pure := NewGraph(nil, map[string][]string{"s": {"s"}})
	if c := pure.Validate(); c == nil {
		t.Error("pure self-reference must be refused: it can never resolve")
	}
}

// A long chain that does not loop must be accepted: the walk is bounded by the
// node set, not by a depth cap, so legitimate deep nesting is not refused.
func TestValidateAcceptsDeepChain(t *testing.T) {
	combos := map[string][]string{}
	for i := 0; i < 200; i++ {
		combos[name(i)] = []string{name(i + 1)}
	}
	combos[name(200)] = []string{"deepseek/deepseek-chat"}
	if c := NewGraph(nil, combos).Validate(); c != nil {
		t.Fatalf("deep acyclic chain refused: %s", Describe(c))
	}
}

func name(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "n0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return "n" + string(b)
}
