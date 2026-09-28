package clisetup

import (
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Every installer writes its config through writeFile, which keeps the file
// exactly as it was before 9router first touched it as "<path>.9router.bak".
// Reset reads that backup and puts the operator's own values back, instead of
// deleting the keys 9router wrote and taking the operator's values with them.
//
// Two kinds of managed key are told apart. A key 9router merely overwrote
// (a base URL, a model id, a provider choice) holds a user value in the backup
// and is restored. A key inside 9router's own namespace ("9router" provider
// tables, "9router/"-prefixed model ids, the "9Router" Copilot entry) is
// 9router's by construction, so it is removed even when the backup has a copy:
// putting it back would resurrect a stale gateway config and leave the tool
// pointed at 9router after a reset.

// backupSuffix is appended to a config path to hold the pre-9router original.
const backupSuffix = ".9router.bak"

// backupPath returns the install-time backup of a config file.
func backupPath(path string) string { return path + backupSuffix }

// subMap walks nested tables, returning nil as soon as a level is missing or
// is not an object. With no keys it returns doc itself (nil when there is no
// backup at all).
func subMap(doc map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m, ok := doc[k].(map[string]any)
		if !ok {
			return nil
		}
		doc = m
	}
	return doc
}

// resetPlan carries the pre-install state a reset restores from. A zero plan
// (no usable backup) makes every restore fall back to deletion, which is all
// an install from an older version, or a deleted backup, can offer.
type resetPlan struct {
	doc  map[string]any // decoded backup of a JSON/TOML config
	text string         // raw backup of a line-oriented config
	raw  bool           // a usable backup exists
}

func planJSON(path string) resetPlan {
	doc := map[string]any{}
	if !readBackupJSON(path, &doc) {
		return resetPlan{}
	}
	return resetPlan{doc: doc, raw: true}
}

func planTOML(path string) resetPlan {
	doc := map[string]any{}
	if !readBackupTOML(path, &doc) {
		return resetPlan{}
	}
	return resetPlan{doc: doc, raw: true}
}

// planText keeps the backup verbatim for configs that are edited line by line
// (Hermes' YAML, Grok's TOML), where only the managed block is replaced.
func planText(path string) resetPlan {
	text, ok := readBackupText(path)
	if !ok {
		return resetPlan{}
	}
	return resetPlan{text: text, raw: true}
}

// ok reports whether a usable pre-install backup was found.
func (p resetPlan) ok() bool { return p.raw }

// mode reports how the reset should describe itself to the operator.
func (p resetPlan) mode() resetMode {
	if p.ok() {
		return modeRestored
	}
	return modeNoBackup
}

// region returns the backup's view of a nested table, nil when absent.
func (p resetPlan) region(keys ...string) map[string]any { return subMap(p.doc, keys...) }

// had reports whether the backup contained a nested table at all, which tells
// an emptied table (restore it empty) from an absent one (drop it).
func (p resetPlan) had(keys ...string) bool { return p.region(keys...) != nil }

// restoreKeys puts the pre-install value of each key back into cur. A key the
// install introduced — absent from prev, including every key when there is no
// backup — is removed, which is the correct undo for it.
func restoreKeys(cur, prev map[string]any, keys ...string) {
	for _, k := range keys {
		restoreValue(cur, prev, k, nil)
	}
}

// restoreValue restores one key from prev. A backed-up value that is itself a
// 9router value (ours reports true) is not the operator's, so it is removed
// rather than put back. Without a predicate every backed-up value is taken to
// be the operator's, which is the safer default for a value we cannot classify.
func restoreValue(cur, prev map[string]any, key string, ours func(any) bool) {
	v, ok := prev[key]
	if !ok || (ours != nil && ours(v)) {
		delete(cur, key)
		return
	}
	cur[key] = v
}

// pruneRestored drops an emptied container unless the backup had it, in which
// case an empty container is the pre-install state.
func pruneRestored(m map[string]any, key string, had bool) {
	if had {
		return
	}
	pruneEmpty(m, key)
}

// is9RouterModelID reports whether a value is a model id in 9router's
// namespace, e.g. "9router/cx/gpt-5".
func is9RouterModelID(v any) bool {
	s, _ := v.(string)
	return strings.HasPrefix(s, "9router/")
}

// resetMode says what a reset could do about the operator's prior values.
type resetMode int

const (
	// modeRestored: the prior values came back from the install backup.
	modeRestored resetMode = iota
	// modeNoBackup: there was no backup, so prior values were lost; the
	// message has to say so rather than imply a clean restore.
	modeNoBackup
	// modeNamespaceOnly: the installer only ever wrote keys in 9router's own
	// namespace, so removing them is already the exact inverse and no prior
	// operator value was involved.
	modeNamespaceOnly
)

// resetMessage describes a reset in terms the operator can act on. It names the
// backup the prior values came from, and says plainly when they could not be
// restored.
func resetMessage(what, path string, mode resetMode) string {
	base := filepath.Base(backupPath(path))
	switch mode {
	case modeRestored:
		return what + " Prior values restored from " + base + "."
	case modeNamespaceOnly:
		return what + " 9router's own settings were removed; your other settings are untouched."
	default:
		return what + " No " + base + " backup was found, so prior values could not be restored."
	}
}

// readBackupJSON decodes the pre-9router backup of path into v. It reports
// false when there is no backup or it does not parse, so callers fall back to
// plain deletion instead of guessing.
func readBackupJSON(path string, v any) bool {
	b, err := readFile(backupPath(path))
	if err != nil || b == nil {
		return false
	}
	return decodeJSON(b, v) == nil
}

func readBackupTOML(path string, v any) bool {
	b, err := readFile(backupPath(path))
	if err != nil || b == nil {
		return false
	}
	return toml.Unmarshal(b, v) == nil
}

// readBackupText returns the backup as text and whether it was readable.
func readBackupText(path string) (string, bool) {
	b, err := readFile(backupPath(path))
	if err != nil || b == nil {
		return "", false
	}
	return string(b), true
}

// restoreEnvLine puts a KEY=value line back the way the backup had it, or
// removes it when the key was not there before.
func restoreEnvLine(text, key, prev string, had bool) string {
	if had {
		return upsertEnvLine(text, key, prev)
	}
	return removeEnvLine(text, key)
}

func removeEnvLine(text, key string) string {
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `=.*\r?\n?`)
	return re.ReplaceAllLiteralString(text, "")
}

// envLine returns the value of key in a KEY=value file and whether it exists.
func envLine(text, key string) (string, bool) {
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `=(.*)$`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}
