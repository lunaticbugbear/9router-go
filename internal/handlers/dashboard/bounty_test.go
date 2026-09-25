package dashboard

import (
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/bounty"
)

func withBountyID(r *http.Request, id string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

func TestBountyProfileCRUDAndPreview(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	body := `{"program":"Demo","programUrl":"https://hackerone.com/demo","inScope":["api.demo.test"],"outOfScope":["billing.demo.test"],"rules":"No destructive tests."}`
	put := httptest.NewRecorder()
	h.HandlePutBountyProfile(put, withBountyID(httptest.NewRequest(http.MethodPut, "/api/bounty/profiles/h1-demo", strings.NewReader(body)), "h1-demo"))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT: status=%d body=%s", put.Code, put.Body.String())
	}

	list := httptest.NewRecorder()
	h.HandleGetBountyProfiles(list, httptest.NewRequest(http.MethodGet, "/api/bounty/profiles", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "h1-demo") {
		t.Fatalf("GET: status=%d body=%s", list.Code, list.Body.String())
	}

	preview := httptest.NewRecorder()
	h.HandleGetBountyPromptPreview(preview, httptest.NewRequest(http.MethodGet, "/api/bounty/prompt-preview?profile=h1-demo", nil))
	var previewBody map[string]any
	if err := json.Unmarshal(preview.Body.Bytes(), &previewBody); err != nil {
		t.Fatal(err)
	}
	if preview.Code != http.StatusOK || previewBody["stored"] != false || !strings.Contains(previewBody["context"].(string), "api.demo.test") {
		t.Fatalf("preview did not show the current prompt context: status=%d body=%s", preview.Code, preview.Body.String())
	}

	del := httptest.NewRecorder()
	h.HandleDeleteBountyProfile(del, withBountyID(httptest.NewRequest(http.MethodDelete, "/api/bounty/profiles/h1-demo", nil), "h1-demo"))
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE: status=%d body=%s", del.Code, del.Body.String())
	}
	missing := httptest.NewRecorder()
	h.HandleGetBountyPromptPreview(missing, httptest.NewRequest(http.MethodGet, "/api/bounty/prompt-preview?profile=h1-demo", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted profile preview should 404, got %d", missing.Code)
	}
}

func TestBountyProfileRejectsEmptyScopeAndMismatchedPathID(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	cases := []struct {
		name, path, body string
	}{
		{"empty scope", "h1-demo", `{"program":"Demo","inScope":[]}`},
		{"mismatched id", "h1-demo", `{"id":"other","program":"Demo","inScope":["api.demo.test"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withBountyID(httptest.NewRequest(http.MethodPut, "/api/bounty/profiles/"+tc.path, strings.NewReader(tc.body)), tc.path)
			h.HandlePutBountyProfile(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBountyHelperReturnsPromptButDoesNotPersistEvidence(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	if err := repo.SetBountyProfile(bounty.Profile{ID: "h1-demo", Program: "Demo", InScope: []string{"api.demo.test"}}); err != nil {
		t.Fatal(err)
	}
	h := NewDashboardHandler(repo)
	evidence := "evidence-must-not-be-stored-9router-test"
	body := `{"profileId":"h1-demo","kind":"report","evidence":"` + evidence + `"}`
	rec := httptest.NewRecorder()
	h.HandleBuildBountyHelper(rec, httptest.NewRequest(http.MethodPost, "/api/bounty/helpers/build", strings.NewReader(body)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), evidence) || !strings.Contains(rec.Body.String(), "api.demo.test") {
		t.Fatalf("helper prompt missing evidence or scope: status=%d body=%s", rec.Code, rec.Body.String())
	}

	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := json.Marshal(raw)
	if strings.Contains(string(stored), evidence) {
		t.Fatal("operator evidence was persisted in the database")
	}
}

func TestBountyHelpersListContainsOnlyReportOrientedTemplates(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)
	rec := httptest.NewRecorder()
	h.HandleGetBountyHelpers(rec, httptest.NewRequest(http.MethodGet, "/api/bounty/helpers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	for _, want := range []string{"scope-check", "safe-plan", "triage", "report"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("helper list missing %s", want)
		}
	}
	if strings.Contains(rec.Body.String(), "payload") {
		t.Fatal("the helper list should not ship exploit payloads")
	}
}
