package dashboard

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"9router/proxy/internal/featureflags"
)

// GET /api/settings/features must serve the entire registry: every flag with
// the state a fresh install actually runs, the metadata the menu renders, and
// the stage label that tells an operator whether the toggle does anything yet.
func TestFeatureFlagsGetServesRegistryDefaults(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	rec := httptest.NewRecorder()
	h.HandleGetFeatureFlags(rec, httptest.NewRequest(http.MethodGet, "/api/settings/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp featureFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	all := featureflags.All()
	if len(resp.Flags) != len(all) {
		t.Fatalf("expected all %d registered flags, got %d", len(all), len(resp.Flags))
	}

	// The response must carry exactly the registry's ids, so a flag dropped by a
	// refactor fails here instead of silently disappearing from the menu.
	byID := make(map[string]featureFlagView, len(resp.Flags))
	gotIDs := make([]string, 0, len(resp.Flags))
	for _, f := range resp.Flags {
		if _, dup := byID[f.ID]; dup {
			t.Fatalf("flag %s served twice", f.ID)
		}
		byID[f.ID] = f
		gotIDs = append(gotIDs, f.ID)
	}
	sort.Strings(gotIDs)
	if got, want := strings.Join(gotIDs, ","), strings.Join(sortedFlagIDs(), ","); got != want {
		t.Fatalf("served flag ids differ from the registry:\n got %s\nwant %s", got, want)
	}

	// Untouched flags resolve to their designed default and say so: `chosen`
	// is what keeps the menu from presenting a default as a decision.
	var stableCount, plannedCount int
	for _, f := range all {
		view := byID[f.ID]
		if view.On != f.Default {
			t.Errorf("%s: on=%v, want registry default %v", f.ID, view.On, f.Default)
		}
		if view.Chosen {
			t.Errorf("%s: never stored, must not report chosen", f.ID)
		}
		if view.Title != f.Title || view.Description != f.Description || view.Category != f.Category {
			t.Errorf("%s: response dropped registry metadata: %+v", f.ID, view)
		}
		switch f.Stage {
		case featureflags.Stable:
			stableCount++
			if view.Stage != "stable" {
				t.Errorf("%s: stage=%q, want stable", f.ID, view.Stage)
			}
		case featureflags.Planned:
			plannedCount++
			if view.Stage != "planned" {
				t.Errorf("%s: stage=%q, want planned", f.ID, view.Stage)
			}
		}
	}
	if resp.StableCount != stableCount || resp.PlannedCount != plannedCount {
		t.Errorf("counts: stable=%d planned=%d, want %d/%d",
			resp.StableCount, resp.PlannedCount, stableCount, plannedCount)
	}

	// Categories come from the registry in display order, because the menu
	// renders its sections straight from this list.
	wantCats := featureflags.Categories()
	if len(resp.Categories) != len(wantCats) {
		t.Fatalf("categories=%v, want %v", resp.Categories, wantCats)
	}
	for i, c := range wantCats {
		if resp.Categories[i] != c {
			t.Fatalf("category %d = %q, want %q", i, resp.Categories[i], c)
		}
	}

	// One pinned flag per stage: the label is the only thing standing between a
	// planned toggle and an operator believing it works.
	if got := byID["prompt.personas"]; got.Stage != "stable" || !got.On {
		t.Errorf("prompt.personas should be a default-on stable flag, got %+v", got)
	}
	if got := byID["api.mcp"]; got.Stage != "planned" || got.On {
		t.Errorf("api.mcp should be a default-off planned flag, got %+v", got)
	}
}

// A stored choice must win over the registry default, in both directions, and
// flags the operator never touched must keep reporting their default.
func TestFeatureFlagsGetReflectsStoredChoice(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	// Turn a planned flag (default off) on and a stable flag (default on) off,
	// so neither result can be explained by the default alone.
	if err := repo.SetFeatureFlag("api.mcp", true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetFeatureFlag("prompt.personas", false); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleGetFeatureFlags(rec, httptest.NewRequest(http.MethodGet, "/api/settings/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp featureFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byID := make(map[string]featureFlagView, len(resp.Flags))
	for _, f := range resp.Flags {
		byID[f.ID] = f
	}

	if got := byID["api.mcp"]; !got.On || !got.Chosen {
		t.Errorf("api.mcp: on=%v chosen=%v, want stored on", got.On, got.Chosen)
	}
	if got := byID["prompt.personas"]; got.On || !got.Chosen {
		t.Errorf("prompt.personas: on=%v chosen=%v, want stored off", got.On, got.Chosen)
	}
	if got := byID["routing.combos"]; !got.On || got.Chosen {
		t.Errorf("routing.combos: on=%v chosen=%v, want untouched default on", got.On, got.Chosen)
	}
}

// PUT stores exactly one choice, echoes what it stored, and refuses anything it
// cannot act on instead of writing a switch that controls nothing.
func TestFeatureFlagsPutStoresChoiceAndRejectsBadInput(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleSetFeatureFlag(rec, httptest.NewRequest(http.MethodPut, "/api/settings/features", strings.NewReader(body)))
		return rec
	}

	ok := put(`{"flag":"api.mcp","on":true}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("valid PUT: status=%d body=%s", ok.Code, ok.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(ok.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if payload["flag"] != "api.mcp" || payload["on"] != true || payload["stage"] != "planned" {
		t.Fatalf("unexpected PUT payload: %v", payload)
	}

	// The choice is persisted, not merely echoed, and it does not clobber the
	// flags it was not asked about.
	flags, err := repo.GetFeatureFlags()
	if err != nil {
		t.Fatal(err)
	}
	if !flags["api.mcp"] {
		t.Error("PUT did not persist the choice")
	}
	if !flags["routing.combos"] {
		t.Error("PUT clobbered an unrelated flag")
	}

	// An explicit `false` is a decision, so it must be stored as one rather
	// than read back as an untouched default.
	off := put(`{"flag":"prompt.personas","on":false}`)
	if off.Code != http.StatusOK {
		t.Fatalf("explicit-off PUT: status=%d body=%s", off.Code, off.Body.String())
	}
	settings, err := repo.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if on, stored := settings.FeatureFlags["prompt.personas"]; !stored || on {
		t.Errorf("explicit false not stored: value=%v present=%v", on, stored)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"unknown flag", `{"flag":"no.such.flag","on":true}`},
		{"missing flag", `{"on":true}`},
		{"malformed JSON", `{"flag":`},
	} {
		if rec := put(tc.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", tc.name, rec.Code, rec.Body.String())
		}
	}

	// A rejected write must not have created a switch for the unknown id.
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if stored, _ := raw["featureFlags"].(map[string]any); stored["no.such.flag"] != nil {
		t.Errorf("rejected PUT stored an unknown flag: %v", stored)
	}
}

// DELETE clears every stored choice and answers with the effective defaults, so
// the menu reflects the reset without a second round trip.
func TestFeatureFlagsDeleteResetsToDefaults(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	if err := repo.SetFeatureFlag("api.mcp", true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetFeatureFlag("prompt.personas", false); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.HandleResetFeatureFlags(rec, httptest.NewRequest(http.MethodDelete, "/api/settings/features", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Flags map[string]bool `json:"flags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode DELETE response: %v", err)
	}
	if len(payload.Flags) != len(featureflags.All()) {
		t.Fatalf("expected %d effective flags, got %d", len(featureflags.All()), len(payload.Flags))
	}
	if payload.Flags["api.mcp"] {
		t.Error("api.mcp still on after reset; the response must already show the default")
	}
	if !payload.Flags["prompt.personas"] {
		t.Error("prompt.personas still off after reset; the response must already show the default")
	}

	// Nothing is left stored, and a later GET agrees: every flag is back to its
	// default and no flag claims to have been chosen.
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	if stored, _ := raw["featureFlags"].(map[string]any); len(stored) != 0 {
		t.Errorf("reset left %d stored choices: %v", len(stored), stored)
	}

	get := httptest.NewRecorder()
	h.HandleGetFeatureFlags(get, httptest.NewRequest(http.MethodGet, "/api/settings/features", nil))
	var resp featureFlagsResponse
	if err := json.Unmarshal(get.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	for _, f := range resp.Flags {
		if f.Chosen {
			t.Errorf("%s still reports a stored choice after reset", f.ID)
		}
		if want, _ := featureflags.Get(f.ID); f.On != want.Default {
			t.Errorf("%s: on=%v after reset, want default %v", f.ID, f.On, want.Default)
		}
	}
}

// Without storage the settings menu must fail loudly rather than answer with a
// fabricated flag list an operator could act on.
func TestFeatureFlagsHandlersFailClosedWithoutRepo(t *testing.T) {
	h := &DashboardHandler{}

	calls := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"GET", h.HandleGetFeatureFlags},
		{"PUT", h.HandleSetFeatureFlag},
		{"DELETE", h.HandleResetFeatureFlags},
	}
	for _, tc := range calls {
		rec := httptest.NewRecorder()
		tc.call(rec, httptest.NewRequest(http.MethodGet, "/api/settings/features", strings.NewReader(`{"flag":"api.mcp","on":true}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status=%d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "settings storage is unavailable") {
			t.Errorf("%s: unexpected error body %s", tc.name, rec.Body.String())
		}
	}
}
