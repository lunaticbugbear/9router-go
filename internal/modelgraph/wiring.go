package modelgraph

import (
	"fmt"

	json "encoding/json/v2"

	"9router/proxy/internal/db"
)

// LoadFromRepo builds the graph from the live stores: aliases from the kv scope
// and combos from the combos table. Both are read because a loop can cross
// between them, which is the shape that caused the original crash.
//
// A store that cannot be read yields an error rather than an empty graph: an
// empty graph would report "no loop" for a config that was never inspected,
// which is the one answer a validator must never invent.
func LoadFromRepo(repo *db.Repo) (Graph, error) {
	if repo == nil {
		return Graph{}, fmt.Errorf("model graph: storage unavailable")
	}
	aliases, err := repo.GetModelAliases()
	if err != nil {
		return Graph{}, fmt.Errorf("model graph: read aliases: %w", err)
	}
	combos, err := repo.GetCombos()
	if err != nil {
		return Graph{}, fmt.Errorf("model graph: read combos: %w", err)
	}
	comboLeaves := make(map[string][]string, len(combos))
	for _, c := range combos {
		if c == nil || c.Name == "" {
			continue
		}
		comboLeaves[c.Name] = parseComboLeaves(c.Models)
	}
	return NewGraph(aliases, comboLeaves), nil
}

// parseComboLeaves decodes a combo's stored leaf list. An unparseable blob is
// treated as no leaves: the row exists (so a same-named alias/combo still
// counts) but contributes no outgoing edges, which matches how resolution treats
// a malformed combo — it fails to expand rather than inventing a target.
func parseComboLeaves(modelsJSON string) []string {
	if modelsJSON == "" {
		return nil
	}
	var leaves []string
	if err := json.Unmarshal([]byte(modelsJSON), &leaves); err != nil {
		return nil
	}
	return leaves
}

// ValidateAliasWriteAgainstRepo refuses an alias write that would close a loop,
// returning an operator-facing message when it must be refused.
//
// The proposal is validated against the state the write would leave behind: the
// alias is replaced (or added) on top of the live graph, so an edit that
// resolves an existing loop is allowed and one that creates a loop is refused.
func ValidateAliasWriteAgainstRepo(repo *db.Repo, alias, target string) error {
	g, err := LoadFromRepo(repo)
	if err != nil {
		return err
	}
	if c := g.ValidateAliasWrite(alias, target); c != nil {
		return fmt.Errorf("%s", Describe(c))
	}
	return nil
}

// ValidateComboWriteAgainstRepo refuses a combo write that would close a loop.
// id is the combo being written, so the proposal replaces that combo's own edges
// rather than layering on top of them.
func ValidateComboWriteAgainstRepo(repo *db.Repo, id, name string, leaves []string) error {
	g, err := LoadFromRepo(repo)
	if err != nil {
		return err
	}
	// An update replaces the row's edges; a create adds them. Removing by name
	// first covers both, because a create has no existing edges to remove.
	g = g.WithoutCombo(name)
	if c := g.ValidateComboWrite(name, leaves); c != nil {
		return fmt.Errorf("%s", Describe(c))
	}
	return nil
}

// ComboLeavesFromRequest extracts the leaf list a combo request carries, so a
// handler can validate the same list it is about to store instead of re-deriving
// it from the marshalled form.
func ComboLeavesFromRequest(models any) []string {
	switch m := models.(type) {
	case nil:
		return nil
	case string:
		return parseComboLeaves(m)
	case []string:
		return m
	case []any:
		out := make([]string, 0, len(m))
		for _, v := range m {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		b, err := json.Marshal(m)
		if err != nil {
			return nil
		}
		return parseComboLeaves(string(b))
	}
}
