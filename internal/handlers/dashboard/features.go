package dashboard

import (
	"io"
	"net/http"
	"sort"

	json "encoding/json/v2"

	"9router/proxy/internal/featureflags"
	"9router/proxy/internal/handlerutil"
)

// featureFlagView is one flag as the settings menu renders it. The effective
// state is resolved server-side so a stale client cannot display a default as
// a stored choice.
type featureFlagView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Stage       string `json:"stage"`
	On          bool   `json:"on"`
	// Chosen reports whether the operator ever set this flag. An untouched
	// flag shows its default; this field is what lets the menu say "default"
	// instead of pretending every value was decided.
	Chosen bool `json:"chosen"`
}

// featureFlagsResponse is the payload of GET /api/settings/features.
type featureFlagsResponse struct {
	Flags        []featureFlagView `json:"flags"`
	Categories   []string          `json:"categories"`
	StableCount  int               `json:"stableCount"`
	PlannedCount int               `json:"plannedCount"`
}

// HandleGetFeatureFlags handles GET /api/settings/features.
//
// Planned flags are included rather than filtered out: the settings menu is
// also the roadmap, and an operator deciding what to enable should see what is
// coming. Their stage is labeled plainly so a toggle that does nothing yet
// cannot be mistaken for a working one.
func (h *DashboardHandler) HandleGetFeatureFlags(w http.ResponseWriter, _ *http.Request) {
	if h.Repo == nil {
		writePlainError(w, http.StatusServiceUnavailable, "settings storage is unavailable")
		return
	}
	settings, err := h.Repo.GetSettings()
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "failed to load settings: "+err.Error())
		return
	}
	stored := settings.FeatureFlags
	if stored == nil {
		stored = map[string]bool{}
	}

	resp := featureFlagsResponse{Categories: featureflags.Categories()}
	for _, f := range featureflags.All() {
		on := f.Default
		chosen := false
		if choice, ok := stored[f.ID]; ok {
			on = choice
			chosen = true
		}
		resp.Flags = append(resp.Flags, featureFlagView{
			ID:          f.ID,
			Title:       f.Title,
			Description: f.Description,
			Category:    f.Category,
			Stage:       f.Stage.String(),
			On:          on,
			Chosen:      chosen,
		})
		if f.Stage == featureflags.Stable {
			resp.StableCount++
		} else {
			resp.PlannedCount++
		}
	}
	if resp.Flags == nil {
		resp.Flags = []featureFlagView{}
	}
	handlerutil.WriteJSON(w, http.StatusOK, resp)
}

// featureFlagUpdate is the body of PUT /api/settings/features.
type featureFlagUpdate struct {
	Flag string `json:"flag"`
	On   bool   `json:"on"`
}

// HandleSetFeatureFlag handles PUT /api/settings/features.
//
// One flag per request: the settings menu is a series of deliberate choices,
// and a bulk write would let a stale client silently reset dozens of switches
// it did not mean to touch.
func (h *DashboardHandler) HandleSetFeatureFlag(w http.ResponseWriter, r *http.Request) {
	if h.Repo == nil {
		writePlainError(w, http.StatusServiceUnavailable, "settings storage is unavailable")
		return
	}
	var body featureFlagUpdate
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writePlainError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Flag == "" {
		writePlainError(w, http.StatusBadRequest, "missing flag id")
		return
	}
	def, ok := featureflags.Get(body.Flag)
	if !ok {
		writePlainError(w, http.StatusBadRequest, "unknown feature flag "+body.Flag)
		return
	}
	if err := h.Repo.SetFeatureFlag(body.Flag, body.On); err != nil {
		writePlainError(w, http.StatusInternalServerError, "failed to store choice: "+err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"flag":  body.Flag,
		"on":    body.On,
		"stage": def.Stage.String(),
	})
}

// HandleResetFeatureFlags handles DELETE /api/settings/features: clears every
// stored choice and returns the effective defaults, so the response already
// reflects the reset without a second round trip.
func (h *DashboardHandler) HandleResetFeatureFlags(w http.ResponseWriter, _ *http.Request) {
	if h.Repo == nil {
		writePlainError(w, http.StatusServiceUnavailable, "settings storage is unavailable")
		return
	}
	if err := h.Repo.ResetFeatureFlags(); err != nil {
		writePlainError(w, http.StatusInternalServerError, "failed to reset choices: "+err.Error())
		return
	}
	flags, err := h.Repo.GetFeatureFlags()
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "failed to reload defaults: "+err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"flags": flags})
}

// sortedFlagIDs is a test helper: registry order is display order, and tests
// assert on sorted ids when checking storage round trips.
func sortedFlagIDs() []string {
	ids := make([]string, 0, len(featureflags.All()))
	for _, f := range featureflags.All() {
		ids = append(ids, f.ID)
	}
	sort.Strings(ids)
	return ids
}
