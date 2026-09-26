// Package modelgraph validates the alias/combo graph an operator builds through
// the dashboard.
//
// Why this exists: model resolution follows aliases and expands combos, so a
// loop in that graph used to recurse until the goroutine stack was exhausted —
// `fatal error: stack overflow`, which recover() cannot intercept and which
// aborts the whole gateway process and every in-flight stream on it. A runtime
// guard now bounds that (internal/handlers/chat's resolveVisits), so a stored
// loop fails one request instead of killing the process. This package is the
// second layer: refuse to store a loop in the first place, so an operator learns
// about the mistake while they are making it rather than from a failing request.
//
// Both layers are needed, and neither replaces the other:
//
//   - Write-time validation cannot see every future edit. Two independently
//     valid writes can compose into a loop (add alias A -> B, then repoint a
//     combo at A), and a database restored from a backup may already contain
//     one. The runtime guard covers those.
//   - The runtime guard only fires when the loop is actually requested. It
//     reports a broken config as a failed request, which is a poor way to learn
//     about a typo. Validation prevents the loop existing.
//
// Aliases live in the kv table (scope "modelAliases") and combos in the combos
// table. The graph therefore spans two stores, and a check over either store
// alone provably cannot see a loop that crosses between them — which is exactly
// the alias -> combo -> alias shape that caused the original crash. So both sets
// are loaded and walked together.
package modelgraph

import (
	"fmt"
	"sort"
	"strings"
)

// Edge kinds, used in error messages so an operator knows which store to edit.
const (
	aliasEdge = "alias"
	comboEdge = "combo"
)

// Graph is the resolution graph: names that point at other names.
//
// Aliases map a bare name to whatever the alias was set to. Combos map a combo
// name to its ordered leaf list. A name may appear in both maps; resolution
// prefers the alias, so validation must consider both possibilities rather than
// assuming one store wins.
type Graph struct {
	Aliases map[string]string
	Combos  map[string][]string
}

// NewGraph builds a Graph from the two stores' contents. It copies its inputs so
// a caller's maps cannot be mutated by validation.
func NewGraph(aliases map[string]string, combos map[string][]string) Graph {
	g := Graph{
		Aliases: make(map[string]string, len(aliases)),
		Combos:  make(map[string][]string, len(combos)),
	}
	for k, v := range aliases {
		g.Aliases[k] = v
	}
	for k, v := range combos {
		leaves := make([]string, len(v))
		copy(leaves, v)
		g.Combos[k] = leaves
	}
	return g
}

// WithAlias returns a copy of the graph with one alias set, so a proposed write
// can be validated without touching the live graph.
func (g Graph) WithAlias(alias, target string) Graph {
	out := NewGraph(g.Aliases, g.Combos)
	out.Aliases[alias] = target
	return out
}

// WithCombo returns a copy of the graph with one combo's leaf list replaced.
func (g Graph) WithCombo(name string, leaves []string) Graph {
	out := NewGraph(g.Aliases, g.Combos)
	cp := make([]string, len(leaves))
	copy(cp, leaves)
	out.Combos[name] = cp
	return out
}

// WithoutAlias returns a copy with one alias removed, so an update can be
// validated against the state the write will actually leave behind.
func (g Graph) WithoutAlias(alias string) Graph {
	out := NewGraph(g.Aliases, g.Combos)
	delete(out.Aliases, alias)
	return out
}

// WithoutCombo returns a copy with one combo removed.
func (g Graph) WithoutCombo(name string) Graph {
	out := NewGraph(g.Aliases, g.Combos)
	delete(out.Combos, name)
	return out
}

// Targets returns the names one name points at, and the edge kind for each.
//
// A name in both stores yields both edges: resolution prefers the alias, but a
// loop through the combo edge is still reachable (a client can name the combo
// directly, and the combo branch is consulted whenever the alias does not
// terminate), so validation must not ignore either.
func (g Graph) targets(name string) []edge {
	var out []edge
	// A self-alias is skipped: resolution refuses to rewrite a name to itself,
	// so it cannot recurse, and reporting it would refuse a write an operator
	// can make harmlessly.
	if target, ok := g.Aliases[name]; ok && target != "" && target != name {
		out = append(out, edge{kind: aliasEdge, to: target})
	}
	// A self-referential combo is only a hazard when it is the ONLY leaf: a
	// combo that names itself among concrete leaves is recovered at runtime
	// (the walk skips the cycle and keeps the concrete leaf), so refusing it
	// would reject a config that works today. A pure self-reference can never
	// resolve to anything, so the write is refused.
	if leaves, ok := g.Combos[name]; ok {
		onlySelf := len(leaves) > 0
		for _, leaf := range leaves {
			if leaf != "" && leaf != name {
				onlySelf = false
				break
			}
		}
		if onlySelf {
			return append(out, edge{kind: comboEdge, to: name})
		}
		for _, leaf := range leaves {
			if leaf != "" && leaf != name {
				out = append(out, edge{kind: comboEdge, to: leaf})
			}
		}
	}
	return out
}

type edge struct {
	kind string
	to   string
}

// Cycle describes one loop found in the graph.
type Cycle struct {
	// Path is the loop in order, starting and ending at the same name, e.g.
	// ["cyc", "cyc-combo", "cyc"].
	Path []string
	// Edges names the edge kind taken out of each Path element, so an operator
	// knows whether to edit an alias or a combo at that step.
	Edges []string
}

// Error renders the cycle the way an operator needs to read it: the names in
// order, with the edge kind between them.
func (c Cycle) Error() string {
	var b strings.Builder
	b.WriteString("model resolution loop: ")
	for i, name := range c.Path {
		if i > 0 {
			b.WriteString(" -")
			b.WriteString(c.Edges[i-1])
			b.WriteString("-> ")
		}
		b.WriteString(name)
	}
	return b.String()
}

// Validate reports the first loop reachable from the given names, or nil when
// the graph is acyclic.
//
// The walk is a depth-first search with a colour per node (white unseen, grey on
// the current path, black finished). A grey target is a loop; a black target is
// a diamond and is skipped, which is what keeps a combo that legitimately names
// the same leaf twice — or two combos sharing a leaf — from being reported as a
// loop. Only names reachable from the seeds are visited, so validating one write
// stays proportional to the affected subgraph rather than the whole config.
//
// Seeds are given explicitly so a caller can validate just the name it is about
// to write. Passing no seeds means "check the whole graph", which is what a
// restore or a bulk import would want.
func (g Graph) Validate(seeds ...string) *Cycle {
	if len(seeds) == 0 {
		for name := range g.Aliases {
			seeds = append(seeds, name)
		}
		for name := range g.Combos {
			seeds = append(seeds, name)
		}
		// Deterministic order keeps the reported cycle stable across runs,
		// which matters for a test and for an operator comparing two reports.
		sort.Strings(seeds)
	}

	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make(map[string]int, len(g.Aliases)+len(g.Combos))
	var path []string
	var kinds []string

	var walk func(name string) *Cycle
	walk = func(name string) *Cycle {
		colour[name] = grey
		path = append(path, name)
		for _, e := range g.targets(name) {
			switch colour[e.to] {
			case grey:
				// Found the loop. Trim the path to where this name was first
				// entered, so the report shows the loop itself rather than the
				// approach to it.
				start := 0
				for i, n := range path {
					if n == e.to {
						start = i
						break
					}
				}
				loop := make([]string, 0, len(path)-start+1)
				loop = append(loop, path[start:]...)
				loop = append(loop, e.to)
				edges := make([]string, 0, len(loop)-1)
				edges = append(edges, kinds[start:]...)
				edges = append(edges, e.kind)
				return &Cycle{Path: loop, Edges: edges}
			case white:
				kinds = append(kinds, e.kind)
				if c := walk(e.to); c != nil {
					return c
				}
				kinds = kinds[:len(kinds)-1]
			}
		}
		path = path[:len(path)-1]
		colour[name] = black
		return nil
	}

	for _, seed := range seeds {
		if colour[seed] != white {
			continue
		}
		if c := walk(seed); c != nil {
			return c
		}
	}
	return nil
}

// ValidateAliasWrite checks whether setting alias -> target would create a loop.
// It returns a non-nil Cycle when the write must be refused.
func (g Graph) ValidateAliasWrite(alias, target string) *Cycle {
	return g.WithAlias(alias, target).Validate(alias)
}

// ValidateComboWrite checks whether replacing name's leaves would create a loop.
func (g Graph) ValidateComboWrite(name string, leaves []string) *Cycle {
	return g.WithCombo(name, leaves).Validate(name)
}

// Describe renders a cycle as a single operator-facing sentence naming the
// stores involved, for use as an HTTP error body.
func Describe(c *Cycle) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s (edit the %s named there, then retry)", c.Error(), strings.Join(uniqueKinds(c.Edges), " or "))
}

func uniqueKinds(kinds []string) []string {
	seen := make(map[string]bool, len(kinds))
	var out []string
	for _, k := range kinds {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
