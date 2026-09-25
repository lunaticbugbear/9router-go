package db

import (
	"strings"
	"testing"

	"9router/proxy/internal/persona"
)

func TestPersonas_RoundTripAndPreserveOtherSettings(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	// Seed unrelated settings so the read-modify-write is exercised against the
	// failure it would otherwise cause: clobbering token savers or the password
	// while saving a persona.
	if err := repo.UpdateSettingsRaw(map[string]any{"rtkEnabled": true, "password": "bcrypt-hash"}); err != nil {
		t.Fatal(err)
	}

	p := persona.Persona{
		ID:             "terse",
		Name:           "Terse replies",
		SystemPrompt:   "Answer in one sentence. Prefer concrete nouns.",
		AppendExisting: true,
	}
	if err := repo.SetPersona(p); err != nil {
		t.Fatalf("SetPersona: %v", err)
	}
	// A second persona must not disturb the first.
	if err := repo.SetPersona(persona.Persona{ID: "strict", SystemPrompt: "Refuse ambiguous requests."}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetPersona("terse")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if got == nil || got.Name != p.Name || got.SystemPrompt != p.SystemPrompt || !got.AppendExisting {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Personas) != 2 {
		t.Fatalf("expected 2 personas, got %d", len(settings.Personas))
	}
	if !settings.RTKEnabled {
		t.Fatal("saving a persona clobbered an unrelated setting")
	}
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if raw["password"] != "bcrypt-hash" {
		t.Fatalf("saving a persona clobbered the password: %v", raw["password"])
	}
}

func TestDeletePersonaRemovesItAndReportsDanglingDefault(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPersonasPlane(true, "terse"); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeletePersona("terse"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetPersona("terse")
	if err != nil || got != nil {
		t.Fatalf("expected persona to be gone, got %+v err=%v", got, err)
	}

	// The default is deliberately left pointing at the deleted key: the operator
	// is told, and resolution then fails closed rather than silently serving
	// requests without the persona they configured.
	stillDefault, err := repo.PersonasBindingDefault("terse")
	if err != nil {
		t.Fatal(err)
	}
	if !stillDefault {
		t.Fatal("a dangling default must be reported, not silently cleared")
	}
	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PersonasEnabled || settings.DefaultPersona != "terse" {
		t.Fatalf("plane state changed unexpectedly: enabled=%v default=%q", settings.PersonasEnabled, settings.DefaultPersona)
	}

	// A default naming an unstored persona is also rejected at write time, so the
	// dangling state above can only be reached by deleting afterwards.
	if err := repo.SetPersonasPlane(true, "never-stored"); err == nil {
		t.Fatal("a default naming an unstored persona must be rejected")
	}
	if err := repo.SetPersonasPlane(false, ""); err != nil {
		t.Fatalf("clearing the default must be allowed: %v", err)
	}
	settings, err = repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.PersonasEnabled || settings.DefaultPersona != "" {
		t.Fatalf("plane not disabled/cleared: enabled=%v default=%q", settings.PersonasEnabled, settings.DefaultPersona)
	}
}

func TestSetPersonaRejectsInvalidWithoutPersisting(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	for _, p := range []persona.Persona{
		{ID: "empty-prompt"},
		{ID: "bad id", SystemPrompt: "x"},
		{ID: "oversize", SystemPrompt: strings.Repeat("x", persona.MaxSystemPromptLength)},
	} {
		if err := repo.SetPersona(p); err == nil {
			t.Fatalf("invalid persona %q was accepted", p.ID)
		}
		got, err := repo.GetPersona(p.ID)
		if err != nil || got != nil {
			t.Fatalf("invalid persona %q was persisted: %+v err=%v", p.ID, got, err)
		}
	}
	// Expect the bound to reject the oversize prompt for size, not for emptiness.
	if err := repo.SetPersona(persona.Persona{ID: "oversize", SystemPrompt: strings.Repeat("x", persona.MaxSystemPromptLength)}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected a size error for the oversize persona, got %v", err)
	}
}

// The loader is the only path that reads what is already on disk, so it must
// reject a malformed record under a valid key without erasing its neighbours.
func TestPersonaLoaderSkipsInvalidRecordsPerKey(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	raw["personas"] = map[string]any{
		"good":      map[string]any{"systemPrompt": "be brief"},
		"empty":     map[string]any{"systemPrompt": ""},
		"oversize":  map[string]any{"systemPrompt": strings.Repeat("x", persona.MaxSystemPromptLength)},
		"bad key!!": map[string]any{"systemPrompt": "x"},
		// The map key wins over a stale nested id, so this record must be usable
		// under "renamed" instead of being dropped by validation.
		"renamed": map[string]any{"id": "", "systemPrompt": "keep me"},
	}
	if err := repo.UpdateSettingsRaw(raw); err != nil {
		t.Fatal(err)
	}

	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Personas) != 2 {
		t.Fatalf("expected only the 2 usable records, got %d: %+v", len(settings.Personas), settings.Personas)
	}
	if _, ok := settings.Personas["good"]; !ok {
		t.Fatal("valid persona was dropped")
	}
	kept, ok := settings.Personas["renamed"]
	if !ok || kept.ID != "renamed" || kept.SystemPrompt != "keep me" {
		t.Fatalf("map key did not override the nested id: %+v", kept)
	}
}

func TestPersonasPlaneRequiresKnownDefaultBeforeEnabling(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.PersonasEnabled || settings.DefaultPersona != "" {
		t.Fatalf("the persona plane must default to off with no default: %+v", settings)
	}

	if err := repo.SetPersona(persona.Persona{ID: "terse", SystemPrompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPersonasPlane(true, "terse"); err != nil {
		t.Fatalf("enabling with a stored default failed: %v", err)
	}
	settings, err = repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PersonasEnabled || settings.DefaultPersona != "terse" {
		t.Fatalf("plane state not persisted: enabled=%v default=%q", settings.PersonasEnabled, settings.DefaultPersona)
	}
}
