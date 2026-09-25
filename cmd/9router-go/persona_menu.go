package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"9router/proxy/internal/db"
	"9router/proxy/internal/persona"
)

// PersonaStore is the slice of the settings repository the persona loader needs.
// It is an interface so the submenu's dispatch is testable with a map-backed
// stub; the real implementation is *db.Repo over the shared SQLite handle the
// gateway already has open.
type PersonaStore interface {
	GetSettings() (*db.SettingsData, error)
	SetPersonasPlane(enabled bool, defaultPersona string) error
}

// personaMenuAction is the dispatch of one persona-submenu line. Like
// parseMenuChoice it is pure, so the submenu's behavior is table-testable
// without driving real I/O.
type personaMenuAction struct {
	Kind personaMenuKind
	// Index is the 1-based list position for a "pick a default" line; 0 when the
	// line is not a pick.
	Index int
	// Text is the persona id to store for a "create" line; empty when the line
	// is not a create.
	Text string
}

// personaMenuKind enumerates the persona-submenu outcomes.
type personaMenuKind int

const (
	personaMenuUnknown personaMenuKind = iota
	personaMenuBack
	personaMenuTogglePlane
	personaMenuPickDefault
	personaMenuCreate
	personaMenuPickDefaultNone
)

// parsePersonaMenuLine maps one submenu line to an action.
//
// Numbered lines select the persona currently at that list position, which is
// why the parse only recovers the number: the caller resolves it against the
// list it just printed. That keeps a stale number from silently applying to a
// different persona than the operator saw.
func parsePersonaMenuLine(raw string) personaMenuAction {
	line := strings.TrimSpace(raw)
	switch line {
	case "b", "B", "back":
		return personaMenuAction{Kind: personaMenuBack}
	case "t", "T", "toggle":
		return personaMenuAction{Kind: personaMenuTogglePlane}
	case "n", "N", "none":
		return personaMenuAction{Kind: personaMenuPickDefaultNone}
	}
	lower := strings.ToLower(line)
	switch {
	case lower == "c", lower == "d", lower == "r":
		// The bare letter is a usage error, not a create/pick with an empty
		// operand: treating it as a create would ask for prompt text and then
		// fail validation, and treating it as a pick would look up position 0.
		return personaMenuAction{Kind: personaMenuUnknown}
	case strings.HasPrefix(lower, "c "):
		return personaMenuAction{Kind: personaMenuCreate, Text: strings.TrimSpace(line[2:])}
	case strings.HasPrefix(lower, "d "):
		return personaMenuAction{Kind: personaMenuPickDefault, Index: parsePersonaIndex(line[2:])}
	case strings.HasPrefix(lower, "r "):
		// "r" resolves to a default pick too, so an operator can read it as
		// "refer to" without changing what the digits mean.
		return personaMenuAction{Kind: personaMenuPickDefault, Index: parsePersonaIndex(line[2:])}
	default:
		return personaMenuAction{Kind: personaMenuUnknown}
	}
}

// parsePersonaIndex accepts a 1-based list number and rejects anything else,
// including 0 and non-numeric text.
func parsePersonaIndex(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 999 {
			return 0
		}
	}
	if n == 0 {
		return 0
	}
	return n
}

// personaListLine renders one list row, marking the current default.
func personaListLine(index int, p persona.Persona, isDefault bool) string {
	marker := "  "
	if isDefault {
		marker = "* "
	}
	mode := "replace"
	if p.AppendExisting {
		mode = "append"
	}
	return fmt.Sprintf("%s%d) %s (%s, %d bytes)\n", marker, index, p.ID, mode, p.AdditionLength())
}

// sortedPersonaIDs returns stored persona keys in a stable display order.
func sortedPersonaIDs(store PersonaStore) ([]string, *db.SettingsData, error) {
	settings, err := store.GetSettings()
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(settings.Personas))
	for id := range settings.Personas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, settings, nil
}

// personaSubmenuText is the fixed submenu help block.
func personaSubmenuText() string {
	return "\nPersona loader\n" +
		"  t) Toggle persona plane on/off\n" +
		"  d <number>) Set the default persona\n" +
		"  n) Clear the default persona\n" +
		"  c <id>) Create a persona from this terminal\n" +
		"  b) Back\n"
}

// runPersonaSubmenu drives the persona menu until the operator backs out. It
// returns true when a shutdown request arrived while the submenu was open, so
// the caller unwinds the outer menu too.
//
// Input arrives through the shared menuSource, never directly from stdin: the
// menu's reader goroutine owns the buffered reader, so a second direct reader
// would race it and lose keystrokes.
func runPersonaSubmenu(opts launcherOptions, src *menuSource, out io.Writer) (stop bool) {
	store := opts.Personas
	if store == nil {
		fmt.Fprintln(out, "Persona storage is unavailable; manage personas from the dashboard.")
		return false
	}

	for {
		ids, settings, err := sortedPersonaIDs(store)
		if err != nil {
			fmt.Fprintf(out, "Failed to read personas: %v\n", err)
			return false
		}
		printPersonaPlane(out, ids, settings)

		fmt.Fprint(out, personaSubmenuText())
		line, stop, err := src.readPrompt("Persona> ", out)
		if err != nil || stop {
			return stop
		}
		action := parsePersonaMenuLine(line)
		// The create path needs two more lines, so it reads through the same
		// source rather than reaching for stdin itself.
		if done, stop := applyPersonaMenuAction(store, ids, settings, action, src, out); done || stop {
			return stop
		}
	}
}

// applyPersonaMenuAction performs one submenu action and reports whether the
// submenu should close, plus whether a stop request arrived.
func applyPersonaMenuAction(store PersonaStore, ids []string, settings *db.SettingsData, action personaMenuAction, src *menuSource, out io.Writer) (closeMenu, stop bool) {
	switch action.Kind {
	case personaMenuBack:
		return true, false
	case personaMenuTogglePlane:
		setPersonaPlane(store, !settings.PersonasEnabled, settings.DefaultPersona, out)
	case personaMenuPickDefaultNone:
		setPersonaPlane(store, settings.PersonasEnabled, "", out)
	case personaMenuPickDefault:
		// The number indexes the list the operator just saw; a stale or
		// out-of-range number is refused rather than clamped onto another
		// persona.
		if action.Index < 1 || action.Index > len(ids) {
			fmt.Fprintln(out, "No persona at that number.")
			return false, false
		}
		setPersonaPlane(store, settings.PersonasEnabled, ids[action.Index-1], out)
	case personaMenuCreate:
		return false, createPersona(store, action.Text, src, out)
	default:
		fmt.Fprintln(out, "Unknown choice.")
	}
	return false, false
}

// printPersonaPlane renders the plane state and the stored persona list.
func printPersonaPlane(out io.Writer, ids []string, settings *db.SettingsData) {
	state := "disabled"
	if settings.PersonasEnabled {
		state = "enabled"
	}
	defaultID := strings.TrimSpace(settings.DefaultPersona)
	if defaultID == "" {
		defaultID = "none"
	}
	fmt.Fprintf(out, "\nPersona plane: %s   Default: %s\n", state, defaultID)
	if len(ids) == 0 {
		fmt.Fprintln(out, "  No personas stored yet. Add one from the dashboard or with 'c <id>'.")
		return
	}
	for i, id := range ids {
		fmt.Fprint(out, personaListLine(i+1, settings.Personas[id], id == strings.TrimSpace(settings.DefaultPersona)))
	}
}

// setPersonaPlane writes the plane state through the repository so the same
// validation the dashboard uses applies here too, and reports the outcome.
func setPersonaPlane(store PersonaStore, enabled bool, defaultPersona string, out io.Writer) {
	if err := store.SetPersonasPlane(enabled, defaultPersona); err != nil {
		fmt.Fprintf(out, "Not saved: %v\n", err)
		return
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	if defaultPersona == "" {
		fmt.Fprintf(out, "Persona plane %s; default cleared.\n", state)
		return
	}
	fmt.Fprintf(out, "Persona plane %s; default is %s.\n", state, defaultPersona)
}

// errPersonaNoText reports a create line with no id.
var errPersonaNoText = errors.New("persona id is required after 'c'")

// createPersona prompts for the persona text and stores it. The text lives only
// in the database row: it is never logged and never passed on a command line.
// It reports whether a stop request arrived while prompting.
func createPersona(store PersonaStore, id string, src *menuSource, out io.Writer) (stop bool) {
	if id == "" {
		fmt.Fprintln(out, errPersonaNoText.Error())
		return false
	}
	if !persona.ValidPersonaID(id) {
		fmt.Fprintf(out, "Persona id must be 1-%d letters, digits, '.', '_' or '-'\n", persona.MaxPersonaIDLength)
		return false
	}
	appendLine, stop, err := src.readPrompt("Append below existing system content? [y/N]: ", out)
	if err != nil || stop {
		return stop
	}
	appendExisting := strings.EqualFold(strings.TrimSpace(appendLine), "y")

	fmt.Fprintf(out, "Persona text (single line, up to %d bytes): ", persona.MaxSystemPromptLength)
	text, stop, err := src.readLine()
	if err != nil || stop {
		return stop
	}
	p := persona.Persona{
		ID:             id,
		SystemPrompt:   strings.TrimSpace(text),
		AppendExisting: appendExisting,
	}
	// Store through the repository's write path; the repository validates, so a
	// too-large or empty persona is refused with the same message the dashboard
	// would show.
	if err := storePersona(store, p); err != nil {
		fmt.Fprintf(out, "Not saved: %v\n", err)
		return false
	}
	fmt.Fprintf(out, "Saved persona %s.\n", id)
	return false
}

// storePersona writes one persona through the repository when it supports
// persona writes. PersonaStore is deliberately narrow — the loader only needs
// the plane for its listed operations — so the create path is a separate
// optional capability.
func storePersona(store PersonaStore, p persona.Persona) error {
	writer, ok := store.(interface{ SetPersona(persona.Persona) error })
	if !ok {
		return errors.New("this persona storage cannot save new personas")
	}
	return writer.SetPersona(p)
}
