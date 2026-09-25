package dashboard

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/handlerutil"
	"9router/proxy/internal/persona"
)

const maxPersonaPayloadBytes = 64 << 10

// HandleGetPersonas lists local personas and the plane state. It returns no
// request/response history; this feature stores only the operator's own text.
func (h *DashboardHandler) HandleGetPersonas(w http.ResponseWriter, _ *http.Request) {
	settings, err := h.Repo.GetSettings()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to load personas")
		return
	}
	personas := make([]persona.Persona, 0, len(settings.Personas))
	for _, p := range settings.Personas {
		personas = append(personas, p)
	}
	sort.Slice(personas, func(i, j int) bool { return personas[i].ID < personas[j].ID })
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"personas":       personas,
		"enabled":        settings.PersonasEnabled,
		"defaultPersona": settings.DefaultPersona,
	})
}

// HandlePutPersona stores a persona. The path id is authoritative; accepting a
// different id in the body would make the UI show one persona while the request
// wrote another.
func (h *DashboardHandler) HandlePutPersona(w http.ResponseWriter, r *http.Request) {
	body, err := readPersonaBody(r)
	if err != nil {
		status, message := personaBodyError(err)
		handlerutil.WriteJSONError(w, status, message)
		return
	}

	var p persona.Persona
	if err := json.Unmarshal(body, &p); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid persona JSON")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "name"))
	if p.ID != "" && p.ID != id {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "persona id in body must match URL id")
		return
	}
	p.ID = id
	if err := p.Validate(); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Repo.SetPersona(p); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to save persona")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "persona": p})
}

// HandleDeletePersona removes one persona. It never silently rewrites
// `defaultPersona`: the response reports whether the default still references
// the removed key, so the operator can see why requests would now fail closed
// instead of discovering it from a 500 later.
func (h *DashboardHandler) HandleDeletePersona(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "name"))
	stored, err := h.Repo.GetPersona(id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read persona")
		return
	}
	if stored == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "persona not found")
		return
	}
	if err := h.Repo.DeletePersona(id); err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to delete persona")
		return
	}
	stillDefault, err := h.Repo.PersonasBindingDefault(id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read persona plane settings")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":                  true,
		"deleted":                  id,
		"defaultStillReferencesIt": stillDefault,
	})
}

// HandlePutPersonasPlane enables or disables the persona plane and sets its
// default persona. Validation is fail-closed: a default that names an unstored
// persona is rejected here rather than breaking every later request.
func (h *DashboardHandler) HandlePutPersonasPlane(w http.ResponseWriter, r *http.Request) {
	body, err := readPersonaBody(r)
	if err != nil {
		status, message := personaBodyError(err)
		handlerutil.WriteJSONError(w, status, message)
		return
	}
	var input struct {
		Enabled        *bool  `json:"enabled"`
		DefaultPersona string `json:"defaultPersona"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid persona plane JSON")
		return
	}
	if input.Enabled == nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if err := h.Repo.SetPersonasPlane(*input.Enabled, input.DefaultPersona); err != nil {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := h.Repo.GetSettings()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read persona plane settings")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"enabled":        settings.PersonasEnabled,
		"defaultPersona": settings.DefaultPersona,
	})
}

// HandleGetPersonaPreview returns the exact system-prompt addition for a
// persona, plus the resolved defaults state. It applies nothing: the preview
// describes what a request would receive, and stores nothing.
func (h *DashboardHandler) HandleGetPersonaPreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("name"))
	stored, err := h.Repo.GetPersona(id)
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read persona")
		return
	}
	if stored == nil {
		handlerutil.WriteJSONError(w, http.StatusNotFound, "persona not found")
		return
	}
	settings, err := h.Repo.GetSettings()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, "failed to read persona plane settings")
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"persona":        stored.ID,
		"header":         personaHeaderName,
		"addition":       stored.BuildSystemAddition(),
		"replace":        !stored.AppendExisting,
		"planeEnabled":   settings.PersonasEnabled,
		"defaultPersona": settings.DefaultPersona,
		"stored":         false,
	})
}

// personaHeaderName mirrors the chat handler's selector header without importing
// it: the dashboard package only needs the literal name for display, and the
// dashboard router does not otherwise depend on the chat internals.
const personaHeaderName = "X-9Router-Persona"

// readPersonaBody reads a bounded dashboard payload. The bound is shared by the
// persona and plane endpoints so both reject an oversized body identically.
func readPersonaBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPersonaPayloadBytes+1))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if len(body) > maxPersonaPayloadBytes {
		return nil, errPersonaPayloadTooLarge
	}
	return body, nil
}

// errPersonaPayloadTooLarge reports a payload past maxPersonaPayloadBytes.
var errPersonaPayloadTooLarge = errors.New("persona payload exceeds 64 KiB")

// personaBodyError maps a body-read failure to its response status and message.
func personaBodyError(err error) (int, string) {
	if errors.Is(err, errPersonaPayloadTooLarge) {
		return http.StatusRequestEntityTooLarge, "persona payload exceeds 64 KiB"
	}
	return http.StatusBadRequest, "failed to read persona payload"
}
