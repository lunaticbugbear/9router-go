package clisetup

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Grok Build's config.toml is edited line by line (like upstream's
// grokBuildConfig.js) so comments and unrelated tables survive untouched.

const grokSlot = "9router"

var (
	grokSubagentTypes = []string{"general-purpose", "explore", "plan"}
	grokPrevDefault   = regexp.MustCompile(`^# 9router-prev-default = "([^"]*)"\s*$`)
	grokPrevSubagent  = regexp.MustCompile(`^# 9router-prev-subagent-([a-z-]+) = "([^"]*)"\s*$`)
)

const grokUnset = "__9router_unset__"

func grokPath(home string) string { return filepath.Join(home, ".grok", "config.toml") }

// tomlSection returns the header index and the exclusive end of a [name]
// table (trailing blank/comment lines excluded, they belong to what follows).
func tomlSection(lines []string, name string) (int, int) {
	hdr := "[" + name + "]"
	for i, l := range lines {
		if strings.TrimSpace(l) != hdr {
			continue
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "[") {
			j++
		}
		for j > i+1 {
			t := strings.TrimSpace(lines[j-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			j--
		}
		return i, j
	}
	return -1, -1
}

func tomlKeyLine(line, key string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, key) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(t[len(key):]), "=")
}

func tomlGet(lines []string, section, key string) string {
	s, e := tomlSection(lines, section)
	for i := s + 1; s >= 0 && i < e; i++ {
		if tomlKeyLine(lines[i], key) {
			v := strings.TrimSpace(lines[i][strings.Index(lines[i], "=")+1:])
			if len(v) >= 2 && v[0] == '"' {
				if end := strings.Index(v[1:], `"`); end >= 0 {
					return v[1 : end+1]
				}
			}
		}
	}
	return ""
}

func tomlSet(lines []string, section, key, value string) []string {
	line := key + ` = "` + value + `"`
	s, e := tomlSection(lines, section)
	if s < 0 {
		return append(lines, "", "["+section+"]", line)
	}
	for i := s + 1; i < e; i++ {
		if tomlKeyLine(lines[i], key) {
			lines[i] = line
			return lines
		}
	}
	return insertLines(lines, s+1, line)
}

func tomlDelete(lines []string, section, key string) []string {
	s, e := tomlSection(lines, section)
	for i := s + 1; s >= 0 && i < e; i++ {
		if tomlKeyLine(lines[i], key) {
			lines = append(lines[:i], lines[i+1:]...)
			e--
			break
		}
	}
	if s >= 0 {
		empty := true
		for i := s + 1; i < e; i++ {
			if t := strings.TrimSpace(lines[i]); t != "" && !strings.HasPrefix(t, "#") {
				empty = false
			}
		}
		if empty {
			lines = append(lines[:s], lines[e:]...)
		}
	}
	return lines
}

func tomlRemoveSection(lines []string, name string) []string {
	s, e := tomlSection(lines, name)
	if s < 0 {
		return lines
	}
	return append(lines[:s], lines[e:]...)
}

func insertLines(lines []string, at int, add ...string) []string {
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:at]...)
	out = append(out, add...)
	return append(out, lines[at:]...)
}

func removeMatching(lines []string, re *regexp.Regexp) ([]string, [][]string) {
	var found [][]string
	kept := lines[:0]
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			found = append(found, m)
			continue
		}
		kept = append(kept, l)
	}
	return kept, found
}

func collapseBlank(lines []string) string {
	var out []string
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	text := strings.Trim(strings.Join(out, "\n"), "\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}

func applyGrok(home string, o Options) (Result, error) {
	path := grokPath(home)
	b, err := readFile(path)
	if err != nil {
		return Result{}, err
	}
	lines := strings.Split(string(b), "\n")
	hasMarker := false
	for _, l := range lines {
		hasMarker = hasMarker || grokPrevDefault.MatchString(l)
	}
	prev := tomlGet(lines, "models", "default")

	block := []string{
		"[model." + grokSlot + "]",
		`model = "` + o.firstModel() + `"`,
		`base_url = "` + o.v1() + `"`,
		`name = "9Router"`,
		`description = "Routed via 9Router gateway"`,
		`api_backend = "chat_completions"`,
		`api_key = "` + o.APIKey + `"`,
	}
	if s, e := tomlSection(lines, "model."+grokSlot); s >= 0 {
		lines = append(lines[:s], append(block, lines[e:]...)...)
	} else {
		lines = append(lines, append([]string{""}, block...)...)
	}
	if !hasMarker && prev != "" && prev != grokSlot {
		s, _ := tomlSection(lines, "model."+grokSlot)
		lines = insertLines(lines, s, `# 9router-prev-default = "`+prev+`"`)
	}
	lines = tomlSet(lines, "models", "default", grokSlot)

	bak, err := writeFile(path, []byte(collapseBlank(lines)))
	return Result{Message: "Grok Build default model slot now routes through 9router.", ConfigPath: path, BackupPath: bak}, err
}

func resetGrok(home string) (Result, error) {
	path := grokPath(home)
	b, err := readFile(path)
	if err != nil || b == nil {
		return Result{Message: "No Grok Build config to reset", ConfigPath: path}, err
	}
	lines := strings.Split(string(b), "\n")

	lines, subs := removeMatching(lines, grokPrevSubagent)
	prevSub := map[string]string{}
	for _, m := range subs {
		prevSub[m[1]] = m[2]
	}
	for _, typ := range grokSubagentTypes {
		slot := grokSlot + "-" + typ
		lines = tomlRemoveSection(lines, "model."+slot)
		if tomlGet(lines, "subagents.models", typ) != slot {
			continue
		}
		if p, ok := prevSub[typ]; ok && p != grokUnset {
			lines = tomlSet(lines, "subagents.models", typ, p)
		} else {
			lines = tomlDelete(lines, "subagents.models", typ)
		}
	}

	lines, defs := removeMatching(lines, grokPrevDefault)
	prev := "grok-build"
	if len(defs) > 0 && defs[0][1] != "" {
		prev = defs[0][1]
	}
	lines = tomlRemoveSection(lines, "model."+grokSlot)
	if tomlGet(lines, "models", "default") == grokSlot {
		lines = tomlSet(lines, "models", "default", prev)
	}
	_, err = writeFile(path, []byte(collapseBlank(lines)))
	return Result{Message: "9router model slots removed from Grok Build", ConfigPath: path}, err
}

func grokConfigured(home string) (bool, string) {
	path := grokPath(home)
	b, err := readFile(path)
	if err != nil || b == nil {
		return false, path
	}
	return tomlGet(strings.Split(string(b), "\n"), "model."+grokSlot, "base_url") != "", path
}
