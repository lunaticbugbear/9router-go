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

// tomlLookup returns a key's unquoted value and whether the key is present.
func tomlLookup(lines []string, section, key string) (string, bool) {
	s, e := tomlSection(lines, section)
	for i := s + 1; s >= 0 && i < e; i++ {
		if !tomlKeyLine(lines[i], key) {
			continue
		}
		v := strings.TrimSpace(lines[i][strings.Index(lines[i], "=")+1:])
		if len(v) >= 2 && v[0] == '"' {
			if end := strings.Index(v[1:], `"`); end >= 0 {
				return v[1 : end+1], true
			}
		}
		return "", true
	}
	return "", false
}

func tomlGet(lines []string, section, key string) string {
	v, _ := tomlLookup(lines, section, key)
	return v
}

// tomlSectionText copies a section out of a document, header included.
func tomlSectionText(lines []string, name string) ([]string, bool) {
	s, e := tomlSection(lines, name)
	if s < 0 {
		return nil, false
	}
	return append([]string{}, lines[s:e]...), true
}

// tomlRestoreSection replaces a section with the backup's version of it, or
// drops it when the backup had no such section. The position of an existing
// section is kept so a reset does not shuffle the operator's file.
func tomlRestoreSection(lines []string, name string, prev []string, had bool) []string {
	s, e := tomlSection(lines, name)
	if s < 0 {
		if !had {
			return lines
		}
		return append(lines, append([]string{""}, prev...)...)
	}
	out := append([]string{}, lines[:s]...)
	if had {
		out = append(out, prev...)
	}
	return append(out, lines[e:]...)
}

// tomlRestoreKey puts a key back the way the backup had it, or removes it when
// the backup did not have it.
func tomlRestoreKey(lines []string, section, key string, prev []string) []string {
	if v, ok := tomlLookup(prev, section, key); ok {
		return tomlSet(lines, section, key, v)
	}
	return tomlDelete(lines, section, key)
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

// grokSlotRef names one key applyGrok can own: a TOML section and, for a
// scalar, the key inside it. An empty key means the section itself is managed.
// ours is the value applyGrok writes, so a key the operator has since pointed
// somewhere else is left alone.
type grokSlotRef struct{ section, key, ours string }

// grokSlots lists every slot applyGrok can own: the 9router model table, the
// default-model pointer, and each per-subagent table and pointer.
func grokSlots() []grokSlotRef {
	out := []grokSlotRef{
		{section: "model." + grokSlot},
		{section: "models", key: "default", ours: grokSlot},
	}
	for _, typ := range grokSubagentTypes {
		out = append(out,
			grokSlotRef{section: "model." + grokSlot + "-" + typ},
			grokSlotRef{section: "subagents.models", key: typ, ours: grokSlot + "-" + typ},
		)
	}
	return out
}

func resetGrok(home string) (Result, error) {
	path := grokPath(home)
	b, err := readFile(path)
	if err != nil || b == nil {
		return Result{Message: "No Grok Build config to reset", ConfigPath: path}, err
	}
	lines := strings.Split(string(b), "\n")
	plan := planText(path)
	// The in-file "# 9router-prev-*" markers are 9router's own record of the
	// values it replaced; they are only consulted when the backup is gone.
	prev := strings.Split(plan.text, "\n")
	prevDefault, prevSub := "", map[string]string{}
	lines, subs := removeMatching(lines, grokPrevSubagent)
	for _, m := range subs {
		prevSub[m[1]] = m[2]
	}
	lines, defs := removeMatching(lines, grokPrevDefault)
	if len(defs) > 0 {
		prevDefault = defs[0][1]
	}
	fromMarkers := false
	if !plan.ok() {
		fromMarkers = prevDefault != "" || len(prevSub) > 0
	}

	for _, slot := range grokSlots() {
		section, key := slot.section, slot.key
		if key == "" {
			// A whole table: the backup decides whether it should exist.
			text, had := tomlSectionText(prev, section)
			lines = tomlRestoreSection(lines, section, text, had && plan.ok())
			continue
		}
		// A key the operator has since pointed somewhere else is theirs now.
		if cur, present := tomlLookup(lines, section, key); present && cur != slot.ours {
			continue
		}
		if !plan.ok() {
			// Without the backup, the markers are the only prior value we have.
			if v, ok := prevSub[key]; ok {
				lines = restoreMarkerValue(lines, section, key, v)
			} else if prevDefault != "" {
				lines = tomlSet(lines, section, key, prevDefault)
			} else {
				lines = tomlDelete(lines, section, key)
			}
			continue
		}
		lines = tomlRestoreKey(lines, section, key, prev)
	}
	if err := writeReset(path, []byte(collapseBlank(lines))); err != nil {
		return Result{}, err
	}
	msg := resetMessage("Grok Build reset.", path, plan.mode())
	if fromMarkers {
		msg = "Grok Build reset. No " + filepath.Base(backupPath(path)) +
			" backup was found, so prior values came from the in-file 9router-prev markers."
	}
	return Result{Message: msg, ConfigPath: path}, nil
}

// restoreMarkerValue applies a marker value, treating grokUnset as "absent".
func restoreMarkerValue(lines []string, section, key, value string) []string {
	if value == grokUnset {
		return tomlDelete(lines, section, key)
	}
	return tomlSet(lines, section, key, value)
}

func grokConfigured(home string) (bool, string) {
	path := grokPath(home)
	b, err := readFile(path)
	if err != nil || b == nil {
		return false, path
	}
	return tomlGet(strings.Split(string(b), "\n"), "model."+grokSlot, "base_url") != "", path
}
