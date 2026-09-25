package main

import (
	"strings"
	"testing"

	"9router/proxy/internal/db"
	"9router/proxy/internal/persona"
)

// Regression: the persona submenu must read through the menu's shared reader.
// When it read stdin directly it raced the menu's reader goroutine, which had
// already buffered the keystrokes — the live TTY run showed "Persona> c terse"
// with no reply at all, because the goroutine had swallowed the line.
func TestRunPersonaSubmenu_ReadsThroughTheSharedMenuReader(t *testing.T) {
	stub := newPersonaStub()
	var out strings.Builder

	// One input stream drives the whole session: open the submenu, create a
	// persona, toggle the plane, go back, then exit the outer menu.
	opts := launcherOptions{
		Port:     20130,
		Out:      &out,
		In:       strings.NewReader("3\nc custom\ny\nbe concise\nt\nb\n4\n"),
		Personas: stub,
	}
	if err := runInteractiveMenu(opts); err != nil {
		t.Fatalf("menu returned error: %v", err)
	}

	stored, ok := stub.personas["custom"]
	if !ok {
		t.Fatalf("the submenu never consumed its input; output:\n%s", out.String())
	}
	if stored.SystemPrompt != "be concise" || !stored.AppendExisting {
		t.Fatalf("create flow stored %+v", stored)
	}
	if !stub.enabled {
		t.Fatalf("toggle did not reach the store; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Persona loader") || !strings.Contains(out.String(), "Persona> ") {
		t.Fatalf("submenu was not drawn:\n%s", out.String())
	}
	// Back must return to the outer menu, which then exits on 4.
	if n := strings.Count(out.String(), "Select [1-4]: "); n != 2 {
		t.Fatalf("expected the outer menu redrawn once (2 prompts), got %d:\n%s", n, out.String())
	}
	// The store's writes must be visible to the submenu's own listing, which
	// reads the same row the gateway reloads per request.
	if !strings.Contains(out.String(), "Persona plane: enabled") {
		t.Fatalf("plane state not reflected in the listing:\n%s", out.String())
	}
}

func TestParsePersonaMenuLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want personaMenuAction
	}{
		{"back letter", "b\n", personaMenuAction{Kind: personaMenuBack}},
		{"back word", "back\n", personaMenuAction{Kind: personaMenuBack}},
		{"toggle", "t\n", personaMenuAction{Kind: personaMenuTogglePlane}},
		{"toggle word", "toggle\n", personaMenuAction{Kind: personaMenuTogglePlane}},
		{"clear default", "n\n", personaMenuAction{Kind: personaMenuPickDefaultNone}},
		{"pick by number", "d 2\n", personaMenuAction{Kind: personaMenuPickDefault, Index: 2}},
		{"bare pick letter is a usage error", "d\n", personaMenuAction{Kind: personaMenuUnknown}},
		{"pick zero is not a position", "d 0\n", personaMenuAction{Kind: personaMenuPickDefault, Index: 0}},
		{"pick non-numeric", "d two\n", personaMenuAction{Kind: personaMenuPickDefault, Index: 0}},
		{"create with id", "c terse\n", personaMenuAction{Kind: personaMenuCreate, Text: "terse"}},
		{"bare create letter is a usage error", "c\n", personaMenuAction{Kind: personaMenuUnknown}},
		{"bare digit is not a submenu action", "2\n", personaMenuAction{Kind: personaMenuUnknown}},
		{"blank", "\n", personaMenuAction{Kind: personaMenuUnknown}},
		{"words", "personas\n", personaMenuAction{Kind: personaMenuUnknown}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePersonaMenuLine(tc.in); got != tc.want {
				t.Errorf("parsePersonaMenuLine(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// newTestSource builds a menuSource over a literal input string, matching the
// production wiring (one reader goroutine, both menus reading through it).
func newTestSource(input string) *menuSource {
	return newMenuSource(strings.NewReader(input), newShutdownTrigger())
}

// personaStoreStub is a map-backed store so submenu dispatch can be tested
// without a database. It records the plane writes it received.
type personaStoreStub struct {
	personas  map[string]persona.Persona
	enabled   bool
	defaultID string
	// planeWrites counts SetPersonasPlane calls so a no-op path is provable.
	planeWrites int
	fail        error
}

func (s *personaStoreStub) GetSettings() (*db.SettingsData, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	return &db.SettingsData{
		Personas:        s.personas,
		PersonasEnabled: s.enabled,
		DefaultPersona:  s.defaultID,
	}, nil
}

func (s *personaStoreStub) SetPersonasPlane(enabled bool, defaultPersona string) error {
	s.planeWrites++
	s.enabled = enabled
	s.defaultID = defaultPersona
	return nil
}

func (s *personaStoreStub) SetPersona(p persona.Persona) error {
	// Mirror the repository's contract: an invalid persona must never be stored.
	if err := p.Validate(); err != nil {
		return err
	}
	s.personas[p.ID] = p
	return nil
}

func newPersonaStub() *personaStoreStub {
	return &personaStoreStub{personas: map[string]persona.Persona{
		"house-style": {ID: "house-style", SystemPrompt: "be brief"},
		"terse":       {ID: "terse", SystemPrompt: "one sentence", AppendExisting: true},
	}}
}

// applyPersonaMenuAction must resolve a numbered pick against the list the
// operator saw, since a stale or out-of-range number would otherwise apply to a
// different persona than the one displayed.
func TestApplyPersonaMenuAction_PickResolvesAgainstPrintedList(t *testing.T) {
	stub := newPersonaStub()
	ids, settings, err := sortedPersonaIDs(stub)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "house-style" || ids[1] != "terse" {
		t.Fatalf("sorted ids = %v", ids)
	}

	var out strings.Builder
	// Position 2 is "terse" in the printed order.
	if close, stop := applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuPickDefault, Index: 2}, newTestSource(""), &out); close || stop {
		t.Fatal("picking a default must not close the submenu")
	}
	if stub.defaultID != "terse" || stub.planeWrites != 1 {
		t.Fatalf("default not set: %q writes=%d", stub.defaultID, stub.planeWrites)
	}
	if !strings.Contains(out.String(), "default is terse") {
		t.Fatalf("expected confirmation, got %q", out.String())
	}

	// An out-of-range number is refused without touching the store.
	before := stub.planeWrites
	out.Reset()
	applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuPickDefault, Index: 9}, newTestSource(""), &out)
	if stub.planeWrites != before {
		t.Fatal("an out-of-range pick wrote to the store")
	}
	if !strings.Contains(out.String(), "No persona at that number.") {
		t.Fatalf("expected a refusal, got %q", out.String())
	}
}

func TestApplyPersonaMenuAction_ToggleAndClear(t *testing.T) {
	stub := newPersonaStub()
	ids, settings, err := sortedPersonaIDs(stub)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder

	// Toggling from disabled enables the plane and leaves the default alone.
	applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuTogglePlane}, newTestSource(""), &out)
	if !stub.enabled {
		t.Fatal("toggle did not enable the plane")
	}
	if !strings.Contains(out.String(), "Persona plane enabled") {
		t.Fatalf("expected an enabled confirmation, got %q", out.String())
	}

	// Toggling again disables it.
	_, settings, _ = sortedPersonaIDs(stub)
	out.Reset()
	applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuTogglePlane}, newTestSource(""), &out)
	if stub.enabled {
		t.Fatal("second toggle did not disable the plane")
	}

	// Clearing the default is explicit and reports it.
	stub.defaultID = "terse"
	_, settings, _ = sortedPersonaIDs(stub)
	out.Reset()
	applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuPickDefaultNone}, newTestSource(""), &out)
	if stub.defaultID != "" {
		t.Fatalf("default not cleared: %q", stub.defaultID)
	}
	if !strings.Contains(out.String(), "default cleared") {
		t.Fatalf("expected a clear confirmation, got %q", out.String())
	}
}

func TestApplyPersonaMenuAction_BackClosesSubmenu(t *testing.T) {
	stub := newPersonaStub()
	ids, settings, _ := sortedPersonaIDs(stub)
	var out strings.Builder
	if close, _ := applyPersonaMenuAction(stub, ids, settings,
		personaMenuAction{Kind: personaMenuBack}, newTestSource(""), &out); !close {
		t.Fatal("'back' must close the submenu")
	}
	if stub.planeWrites != 0 {
		t.Fatal("'back' must not write to the store")
	}
}

// Creating a persona must honour the append/replace prompt and store the text
// through the same validation the dashboard uses.
func TestCreatePersona_ReadsAppendChoiceAndStores(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		input      string
		wantAppend bool
	}{
		{"append chosen", "custom", "y\nbe concise\n", true},
		{"replace chosen", "custom", "n\nbe concise\n", false},
		{"blank answer means replace", "custom", "\nbe concise\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := newPersonaStub()
			var out strings.Builder
			createPersona(stub, tc.id, newTestSource(tc.input), &out)

			stored, ok := stub.personas[tc.id]
			if !ok {
				t.Fatalf("persona not stored; output=%q", out.String())
			}
			if stored.SystemPrompt != "be concise" || stored.AppendExisting != tc.wantAppend {
				t.Fatalf("stored %+v, want prompt %q append=%v", stored, "be concise", tc.wantAppend)
			}
		})
	}
}

func TestCreatePersona_RefusesEmptyIdAndInvalidText(t *testing.T) {
	// No id on the command line: nothing is read from the store.
	stub := newPersonaStub()
	var out strings.Builder
	createPersona(stub, "", newTestSource(""), &out)
	if len(stub.personas) != 2 {
		t.Fatal("an empty id created a persona")
	}
	if !strings.Contains(out.String(), "persona id is required") {
		t.Fatalf("expected a usage message, got %q", out.String())
	}

	// An invalid id is refused before any prompt.
	out.Reset()
	createPersona(stub, "bad id", newTestSource(""), &out)
	if !strings.Contains(out.String(), "Persona id must be") {
		t.Fatalf("expected an id validation message, got %q", out.String())
	}

	// An empty prompt is refused by the repository's validation.
	out.Reset()
	createPersona(stub, "empty-prompt", newTestSource("n\n\n"), &out)
	if _, ok := stub.personas["empty-prompt"]; ok {
		t.Fatal("an empty persona prompt was stored")
	}
	if !strings.Contains(out.String(), "Not saved") {
		t.Fatalf("expected a refusal, got %q", out.String())
	}
}

// The list marker must identify the current default, and the mode must be
// visible so an operator can tell replace from append before choosing.
func TestPersonaListLineMarksDefaultAndMode(t *testing.T) {
	appended := persona.Persona{ID: "terse", SystemPrompt: "x", AppendExisting: true}
	if got := personaListLine(1, appended, false); !strings.Contains(got, "1) terse (append") || strings.HasPrefix(got, "  *") {
		t.Fatalf("unmarked row wrong: %q", got)
	}
	if got := personaListLine(2, appended, true); !strings.Contains(got, "* 2) terse (append") {
		t.Fatalf("default marker missing: %q", got)
	}
	replacing := persona.Persona{ID: "terse", SystemPrompt: "x"}
	if got := personaListLine(1, replacing, false); !strings.Contains(got, "(replace") {
		t.Fatalf("replacement mode not shown: %q", got)
	}
}

// The submenu must report unavailable storage instead of panicking.
func TestRunPersonaSubmenu_NilStoreIsReported(t *testing.T) {
	var out strings.Builder
	runPersonaSubmenu(launcherOptions{}, newTestSource("b\n"), &out)
	if !strings.Contains(out.String(), "Persona storage is unavailable") {
		t.Fatalf("expected an unavailable-storage message, got %q", out.String())
	}
}
