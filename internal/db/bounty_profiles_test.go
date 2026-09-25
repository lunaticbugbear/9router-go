package db

import (
	"strings"
	"testing"

	"9router/proxy/internal/bounty"
)

func TestBountyProfiles_RoundTripAndPreserveOtherSettings(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	// Seed an unrelated setting so the profile helper's read-modify-write is
	// tested against the failure mode it would cause: clobbering token-saver or
	// password settings while saving a bounty scope.
	if err := repo.UpdateSettingsRaw(map[string]any{"rtkEnabled": true, "password": "bcrypt-hash"}); err != nil {
		t.Fatal(err)
	}

	p := bounty.Profile{
		ID:         "h1-example",
		Program:    "Example program",
		ProgramURL: "https://hackerone.com/example",
		InScope:    []string{"api.example.test", "app.example.test"},
		OutOfScope: []string{"billing.example.test"},
		Rules:      "No destructive tests.",
	}
	if err := repo.SetBountyProfile(p); err != nil {
		t.Fatalf("SetBountyProfile: %v", err)
	}

	got, err := repo.GetBountyProfile(p.ID)
	if err != nil {
		t.Fatalf("GetBountyProfile: %v", err)
	}
	if got == nil || got.Program != p.Program || len(got.InScope) != 2 || got.OutOfScope[0] != "billing.example.test" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.RTKEnabled {
		t.Fatal("saving a bounty profile clobbered rtkEnabled")
	}
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if raw["password"] != "bcrypt-hash" {
		t.Fatalf("saving a bounty profile clobbered password: %v", raw["password"])
	}
}

func TestBountyProfiles_DeleteAndMissing(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)
	p := bounty.Profile{ID: "p", Program: "P", InScope: []string{"a.example"}}
	if err := repo.SetBountyProfile(p); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBountyProfile("p"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetBountyProfile("p")
	if err != nil || got != nil {
		t.Fatalf("expected missing after delete, got %+v err=%v", got, err)
	}
}

func TestBountyProfiles_RejectInvalidBeforeStore(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)
	p := bounty.Profile{ID: "empty-scope", Program: "P"}
	if err := repo.SetBountyProfile(p); err == nil {
		t.Fatal("profile with empty in-scope list should be rejected")
	}
	got, err := repo.GetBountyProfile(p.ID)
	if err != nil || got != nil {
		t.Fatalf("invalid profile was persisted: %+v err=%v", got, err)
	}
}

// A profile whose effective context is truncated to fit MaxPromptLength must be
// rejected on load, not served with a clamped context. The old Validate compared
// the already-truncated string, so this case was admitted silently.
func TestBountyProfiles_RejectOversizeOnLoad(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	// Every field sits at its own limit, but together they overflow the effective
	// context budget while each individual check still passes.
	scope := make([]string, bounty.MaxScopeItems)
	for i := range scope {
		scope[i] = strings.Repeat("s", bounty.MaxScopeItemLength)
	}
	oversize := bounty.Profile{
		ID:            "oversize",
		Program:       strings.Repeat("P", bounty.MaxProgramLength),
		ProgramURL:    strings.Repeat("u", bounty.MaxProgramURLLength),
		InScope:       scope,
		OutOfScope:    scope,
		Rules:         strings.Repeat("r", bounty.MaxRulesLength),
		CustomContext: strings.Repeat("c", bounty.MaxRulesLength),
	}
	if err := repo.SetBountyProfile(oversize); err == nil {
		t.Fatal("oversize effective context should not be storable")
	}
	if got, err := repo.GetBountyProfile(oversize.ID); err != nil || got != nil {
		t.Fatalf("oversize profile was persisted: %+v err=%v", got, err)
	}

	// Bypass SetBountyProfile so the loader itself is exercised: write raw JSON
	// containing an oversize record under a valid key.
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	raw["bountyProfiles"] = map[string]any{
		"legit":    map[string]any{"program": "P", "inScope": []string{"a.example"}},
		"oversize": map[string]any{"program": strings.Repeat("P", bounty.MaxProgramLength), "inScope": scope, "rules": strings.Repeat("r", bounty.MaxRulesLength), "customContext": strings.Repeat("c", bounty.MaxRulesLength)},
	}
	if err := repo.UpdateSettingsRaw(raw); err != nil {
		t.Fatal(err)
	}
	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.BountyProfiles["oversize"]; ok {
		t.Fatal("oversize profile was admitted by the loader")
	}
	if _, ok := settings.BountyProfiles["legit"]; !ok {
		t.Fatal("rejecting the oversize profile also dropped a valid sibling")
	}
}

// The stored map key is authoritative. A record whose nested id disagrees must
// still load under its key, otherwise a saved profile disappears from the
// dashboard and a request header naming it fails confusingly.
func TestBountyProfiles_MapKeyIsAuthoritativeOverNestedID(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	raw["bountyProfiles"] = map[string]any{
		"real-key": map[string]any{"id": "", "program": "P", "inScope": []string{"a.example"}},
	}
	if err := repo.UpdateSettingsRaw(raw); err != nil {
		t.Fatal(err)
	}
	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := settings.BountyProfiles["real-key"]
	if !ok {
		t.Fatal("record with empty nested id was dropped instead of using the map key")
	}
	if got.ID != "real-key" {
		t.Fatalf("loaded profile id = %q, want the map key %q", got.ID, "real-key")
	}
}
