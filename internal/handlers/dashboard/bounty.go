package dashboard

import (
	"io"
	"net/http"
	"sort"
	"strings"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/bounty"
	"9router/proxy/internal/handlers/chat"
	"9router/proxy/internal/handlerutil"
)

const maxBountyPayloadBytes = 64 << 10

// HandleGetBountyProfiles lists local, operator-declared scope profiles. It
// returns no request/response history; the feature stores only these profiles.
func (h *DashboardHandler) HandleGetBountyProfiles(w http.ResponseWriter, _ *http.Request) {
	settings, err := h.Repo.GetSettings()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to load bounty profiles")
		return
	}
	profiles := make([]bounty.Profile, 0, len(settings.BountyProfiles))
	for _, p := range settings.BountyProfiles {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

// HandlePutBountyProfile stores a scope profile. The path id is authoritative;
// accepting a different id in the body would make the UI show one profile while
// the request wrote another.
func (h *DashboardHandler) HandlePutBountyProfile(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBountyPayloadBytes+1))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read bounty profile")
		return
	}
	if len(body) > maxBountyPayloadBytes {
		handlerutil.WriteJSONError(w, http.StatusRequestEntityTooLarge, "bounty profile exceeds 64 KiB")
		return
	}
	defer r.Body.Close()

	var profile bounty.Profile
	if err := json.Unmarshal(body, &profile); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid bounty profile JSON")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if profile.ID != "" && profile.ID != id {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "profile id in body must match URL id")
		return
	}
	profile.ID = id
	if err := profile.Validate(); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Repo.SetBountyProfile(profile); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save bounty profile")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "profile": profile})
}

// HandleDeleteBountyProfile removes one profile. A stale client header receives
// an explicit error after deletion; it is never silently served without the
// scope context it expected.
func (h *DashboardHandler) HandleDeleteBountyProfile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	profile, err := h.Repo.GetBountyProfile(id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read bounty profile")
		return
	}
	if profile == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "bounty profile not found")
		return
	}
	if err := h.Repo.DeleteBountyProfile(id); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to delete bounty profile")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "deleted": id})
}

// HandleGetBountyPromptPreview returns the exact scope-context addition for a
// profile. It accepts no client request and stores nothing.
func (h *DashboardHandler) HandleGetBountyPromptPreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("profile"))
	profile, err := h.Repo.GetBountyProfile(id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read bounty profile")
		return
	}
	if profile == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "bounty profile not found")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"profile": profile.ID,
		"header":  chat.BountyProfileHeader,
		"context": profile.BuildSystemContext(),
		"stored":  false,
	})
}

// HandleGetBountyHelpers lists safe, report-oriented task templates. There are
// no built-in exploit payloads; the helper only structures authorized analysis.
func (h *DashboardHandler) HandleGetBountyHelpers(w http.ResponseWriter, _ *http.Request) {
	type helper struct {
		ID          bounty.HelperKind `json:"id"`
		Title       string            `json:"title"`
		Description string            `json:"description"`
	}
	items := []helper{
		{bounty.HelperScopeCheck, "Check scope", "Compare a proposed asset/action with the program scope."},
		{bounty.HelperSafePlan, "Plan safe validation", "Create a minimal, non-destructive verification plan."},
		{bounty.HelperTriage, "Triage evidence", "Assess issue class, demonstrated impact, and missing evidence."},
		{bounty.HelperReport, "Draft HackerOne report", "Structure a report from observed facts, without inventing impact."},
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"helpers": items})
}

// HandleBuildBountyHelper builds a copyable task prompt locally. The evidence is
// processed in memory and returned to the browser; this handler does not store it
// or send it to a provider.
func (h *DashboardHandler) HandleBuildBountyHelper(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBountyPayloadBytes+1))
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "failed to read helper request")
		return
	}
	if len(body) > maxBountyPayloadBytes {
		handlerutil.WriteJSONError(w, http.StatusRequestEntityTooLarge, "helper request exceeds 64 KiB")
		return
	}
	defer r.Body.Close()

	var input struct {
		ProfileID string            `json:"profileId"`
		Kind      bounty.HelperKind `json:"kind"`
		Evidence  string            `json:"evidence"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid helper request JSON")
		return
	}
	profile, err := h.Repo.GetBountyProfile(input.ProfileID)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read bounty profile")
		return
	}
	if profile == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "bounty profile not found")
		return
	}
	task, err := bounty.BuildHelperPrompt(input.Kind, input.Evidence)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"profile":   profile.ID,
		"kind":      input.Kind,
		"prompt":    profile.BuildSystemContext() + "\n\n" + task,
		"persisted": false,
	})
}
