package dashboard

import (
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/persona"
)

func withPersonaName(r *http.Request, name string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add("name", name)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

func TestPersonaCRUDAndPreview(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	body := `{"name":"Terse replies","systemPrompt":"Answer in one sentence.","appendExisting":true}`
	put := httptest.NewRecorder()
	h.HandlePutPersona(put, withPersonaName(httptest.NewRequest(http.MethodPut, "/api/personas/terse", strings.NewReader(body)), "terse"))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", put.Code, put.Body.String())
	}

	list := httptest.NewRecorder()
	h.HandleGetPersonas(list, httptest.NewRequest(http.MethodGet, "/api/personas", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "terse") {
		t.Fatalf("GET: status=%d body=%s", list.Code, list.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed["enabled"] != false || listed["defaultPersona"] != "" {
		t.Fatalf("plane must default to off with no default: %v", listed)
	}

	preview := httptest.NewRecorder()
	h.HandleGetPersonaPreview(preview, httptest.NewRequest(http.MethodGet, "/api/personas/preview?name=terse", nil))
	var previewBody map[string]any
	if err := json.Unmarshal(preview.Body.Bytes(), &previewBody); err != nil {
		t.Fatal(err)
	}
	if preview.Code != http.StatusOK || previewBody["stored"] != false {
		t.Fatalf("preview: status=%d body=%s", preview.Code, preview.Body.String())
	}
	if addition, _ := previewBody["addition"].(string); !strings.Contains(addition, "Answer in one sentence.") ||
		!strings.Contains(addition, "provider safety policies remain authoritative") {
		t.Fatalf("preview did not show the rendered addition: %v", previewBody["addition"])
	}
	if previewBody["replace"] != false {
		t.Fatalf("appendExisting=true must report replace=false: %v", previewBody)
	}

	del := httptest.NewRecorder()
	h.HandleDeletePersona(del, withPersonaName(httptest.NewRequest(http.MethodDelete, "/api/personas/terse", nil), "terse"))
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE: status=%d body=%s", del.Code, del.Body.String())
	}
	missing := httptest.NewRecorder()
	h.HandleGetPersonaPreview(missing, httptest.NewRequest(http.MethodGet, "/api/personas/preview?name=terse", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted persona preview should 404, got %d", missing.Code)
	}
	// Deleting an absent persona must be reported, not silently accepted.
	again := httptest.NewRecorder()
	h.HandleDeletePersona(again, withPersonaName(httptest.NewRequest(http.MethodDelete, "/api/personas/terse", nil), "terse"))
	if again.Code != http.StatusNotFound {
		t.Fatalf("deleting an absent persona should 404, got %d", again.Code)
	}
}

func TestPersonaRejectsInvalidAndMismatchedPathID(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	cases := []struct {
		name, path, body string
	}{
		{"empty prompt", "terse", `{"systemPrompt":""}`},
		{"invalid id in path", "bad id", `{"systemPrompt":"x"}`},
		{"mismatched id", "terse", `{"id":"other","systemPrompt":"x"}`},
		{"invalid json", "terse", `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			// The URL is fixed; the route param is what the handler reads, so an
			// id with characters a real path could not carry is still exercised.
			req := withPersonaName(httptest.NewRequest(http.MethodPut, "/api/personas/name", strings.NewReader(tc.body)), tc.path)
			h.HandlePutPersona(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPersonaPayloadSizeCap(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	huge := `{"systemPrompt":"` + strings.Repeat("x", maxPersonaPayloadBytes+1) + `"}`
	rec := httptest.NewRecorder()
	req := withPersonaName(httptest.NewRequest(http.MethodPut, "/api/personas/huge", strings.NewReader(huge)), "huge")
	h.HandlePutPersona(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for an oversize payload, got %d", rec.Code)
	}
}

func TestPersonaPlaneEndpointFailClosed(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	if err := repo.SetPersona(persona.Persona{ID: "house", SystemPrompt: "be brief"}); err != nil {
		t.Fatal(err)
	}

	// Enabling with a stored default works.
	ok := httptest.NewRecorder()
	h.HandlePutPersonasPlane(ok, httptest.NewRequest(http.MethodPut, "/api/personas/plane", strings.NewReader(`{"enabled":true,"defaultPersona":"house"}`)))
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), "house") {
		t.Fatalf("plane PUT: status=%d body=%s", ok.Code, ok.Body.String())
	}

	// A default naming an unstored persona is refused, so every later request
	// cannot be broken by a typo.
	if err := repo.SetPersonasPlane(false, ""); err != nil {
		t.Fatal(err)
	}
	bad := httptest.NewRecorder()
	h.HandlePutPersonasPlane(bad, httptest.NewRequest(http.MethodPut, "/api/personas/plane", strings.NewReader(`{"enabled":true,"defaultPersona":"typo"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown default must be refused: status=%d body=%s", bad.Code, bad.Body.String())
	}

	// A missing `enabled` field is refused rather than defaulted to false, which
	// would silently disable the plane on a malformed client payload.
	missing := httptest.NewRecorder()
	h.HandlePutPersonasPlane(missing, httptest.NewRequest(http.MethodPut, "/api/personas/plane", strings.NewReader(`{"defaultPersona":"house"}`)))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled must be refused: status=%d body=%s", missing.Code, missing.Body.String())
	}
}

// Deleting the current default must tell the operator the reference is dangling
// instead of silently writing a different plane state.
func TestDeletePersonaReportsDanglingDefault(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	if err := repo.SetPersona(persona.Persona{ID: "house", SystemPrompt: "be brief"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPersonasPlane(true, "house"); err != nil {
		t.Fatal(err)
	}

	del := httptest.NewRecorder()
	h.HandleDeletePersona(del, withPersonaName(httptest.NewRequest(http.MethodDelete, "/api/personas/house", nil), "house"))
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE: status=%d body=%s", del.Code, del.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(del.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["defaultStillReferencesIt"] != true {
		t.Fatalf("dangling default not reported: %v", payload)
	}
}

// Personas must be listed in a stable order so the CLI numbering and the
// dashboard table agree between calls.
func TestPersonaListIsSortedByID(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	for _, id := range []string{"zeta", "alpha", "mid"} {
		if err := repo.SetPersona(persona.Persona{ID: id, SystemPrompt: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	h.HandleGetPersonas(rec, httptest.NewRequest(http.MethodGet, "/api/personas", nil))

	var payload struct {
		Personas []persona.Persona `json:"personas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Personas) != 3 {
		t.Fatalf("expected 3 personas, got %d", len(payload.Personas))
	}
	for i, want := range []string{"alpha", "mid", "zeta"} {
		if payload.Personas[i].ID != want {
			t.Fatalf("position %d = %q, want %q", i, payload.Personas[i].ID, want)
		}
	}
}
