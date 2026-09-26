package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"

	"9router/proxy/internal/db"
)

// withURLParam mirrors the chi-param convention the other dashboard tests use
// (bounty_test.go, persona_test.go), so a route that reads a path parameter can
// be driven directly.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

// The write-time refusal is the second layer over the runtime cycle guard. These
// tests drive the real HTTP handlers, because the guard's value is that an
// operator cannot STORE a looping config through the dashboard — a unit test of
// the validator alone would not prove the handler consults it.

func newGraphTestHandler(t *testing.T) (*DashboardHandler, *db.Repo) {
	t.Helper()
	repo, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	return NewDashboardHandler(repo), repo
}

func postJSON(t *testing.T, h http.HandlerFunc, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code, rec.Body.String()
}

// The exact cross-store loop that used to crash the process: the combo write
// that would close it must be refused with a message naming the loop, and the
// combo must NOT be stored.
func TestCreateComboRefusesCrossStoreLoop(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	// Seed the alias half of the loop: cyc -> cyc-combo.
	valBytes, _ := json.Marshal("cyc-combo")
	if err := repo.SetKV("modelAliases", "cyc", string(valBytes)); err != nil {
		t.Fatal(err)
	}

	// The combo half would close it: cyc-combo -> [cyc].
	code, body := postJSON(t, h.HandleCreateCombo, http.MethodPost, "/api/combos",
		`{"name":"cyc-combo","kind":"fallback","models":["cyc"]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("looping combo write must be refused with 400, got %d: %s", code, body)
	}
	if !strings.Contains(body, "model resolution loop") {
		t.Errorf("refusal must name the loop, got %q", body)
	}
	if !strings.Contains(body, "cyc") {
		t.Errorf("refusal must name the offending names, got %q", body)
	}

	// Nothing was stored.
	combos, err := repo.GetCombos()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range combos {
		if c.Name == "cyc-combo" {
			t.Fatal("the refused combo was stored anyway")
		}
	}
}

// The same refusal on the alias side: the alias write that would close a loop is
// rejected before storage.
func TestSetAliasRefusesCrossStoreLoop(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	if err := repo.CreateCombo("c1", "cyc-combo", "fallback", `["cyc"]`, "fallback"); err != nil {
		t.Fatal(err)
	}

	code, body := postJSON(t, h.HandleSetModelAlias, http.MethodPut, "/api/models/alias",
		`{"alias":"cyc","model":"cyc-combo"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("looping alias write must be refused with 400, got %d: %s", code, body)
	}
	if !strings.Contains(body, "model resolution loop") {
		t.Errorf("refusal must name the loop, got %q", body)
	}
	if target, _ := repo.GetModelAlias("cyc"); target != "" {
		t.Fatalf("the refused alias was stored anyway: %q", target)
	}
}

// A harmless write must still go through: validation that blocks normal work is
// worse than no validation.
func TestValidWritesStillSucceed(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	code, body := postJSON(t, h.HandleCreateCombo, http.MethodPost, "/api/combos",
		`{"name":"team","kind":"fallback","models":["deepseek/deepseek-chat","groq/llama-3.3-70b"]}`)
	if code != http.StatusOK {
		t.Fatalf("valid combo rejected: %d %s", code, body)
	}

	code, body = postJSON(t, h.HandleSetModelAlias, http.MethodPut, "/api/models/alias",
		`{"alias":"fast","model":"deepseek/deepseek-chat"}`)
	if code != http.StatusOK {
		t.Fatalf("valid alias rejected: %d %s", code, body)
	}

	// A diamond — the same leaf twice, and two combos sharing a leaf — is
	// legitimate and must not be mistaken for a loop.
	code, body = postJSON(t, h.HandleCreateCombo, http.MethodPost, "/api/combos",
		`{"name":"diamond","kind":"fallback","models":["shared","shared"]}`)
	if code != http.StatusOK {
		t.Fatalf("diamond rejected: %d %s", code, body)
	}
	if _, err := repo.GetComboByName("diamond"); err != nil {
		t.Fatalf("diamond not stored: %v", err)
	}
}

// An edit that OPENS an existing loop must be allowed: otherwise a config that
// already contains a loop could never be repaired from the dashboard.
func TestEditThatOpensALoopIsAllowed(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	// Seed the loop directly through the repo, bypassing validation, the way a
	// restored backup would.
	if err := repo.CreateCombo("c1", "loopy", "fallback", `["cyc"]`, "fallback"); err != nil {
		t.Fatal(err)
	}
	valBytes, _ := json.Marshal("loopy")
	if err := repo.SetKV("modelAliases", "cyc", string(valBytes)); err != nil {
		t.Fatal(err)
	}

	// Repoint the alias at a concrete model: this breaks the loop and must be
	// accepted.
	code, body := postJSON(t, h.HandleSetModelAlias, http.MethodPut, "/api/models/alias",
		`{"alias":"cyc","model":"deepseek/deepseek-chat"}`)
	if code != http.StatusOK {
		t.Fatalf("repair write was refused: %d %s", code, body)
	}
}

// An unreadable store must not be reported as "no loop": refusing to validate is
// safer than validating against a graph that was never loaded.
func TestValidationFailsClosedWithoutRepo(t *testing.T) {
	h := &DashboardHandler{}
	code, _ := postJSON(t, h.HandleSetModelAlias, http.MethodPut, "/api/models/alias",
		`{"alias":"a","model":"b"}`)
	if code == http.StatusOK {
		t.Fatal("handler reported success with no storage")
	}
}

// A rename must be validated against the edges the write will actually leave
// behind. UpdateCombo rewrites the name, so removing only the NEW name would
// leave the old name's edges in the graph and judge the proposal against a
// config that will not exist — which can refuse a legitimate rename.
func TestRenameComboIsValidatedAgainstPostWriteGraph(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	// "old-name" -> ["cyc"], and alias cyc -> "old-name" closes a loop.
	if err := repo.CreateCombo("c-rename", "old-name", "fallback", `["cyc"]`, "fallback"); err != nil {
		t.Fatal(err)
	}
	valBytes, _ := json.Marshal("old-name")
	if err := repo.SetKV("modelAliases", "cyc", string(valBytes)); err != nil {
		t.Fatal(err)
	}

	// Renaming the combo to something unrelated breaks the loop: this must be
	// ALLOWED, because after the write no combo named "old-name" exists.
	code, body := postComboUpdate(t, h, "c-rename",
		`{"name":"new-name","kind":"fallback","models":["deepseek/deepseek-chat"]}`)
	if code != http.StatusOK {
		t.Fatalf("rename that resolves a loop was refused: %d %s", code, body)
	}

	// The reverse: renaming INTO the loop must be refused. Repoint the alias at
	// the new name so the post-write graph would loop.
	valBytes, _ = json.Marshal("new-name")
	if err := repo.SetKV("modelAliases", "cyc", string(valBytes)); err != nil {
		t.Fatal(err)
	}
	code, body = postComboUpdate(t, h, "c-rename",
		`{"name":"new-name","kind":"fallback","models":["cyc"]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("rename that closes a loop must be refused, got %d: %s", code, body)
	}
	if !strings.Contains(body, "model resolution loop") {
		t.Errorf("refusal must name the loop, got %q", body)
	}
}

// postComboUpdate drives HandleUpdateCombo with its path parameter set.
func postComboUpdate(t *testing.T, h *DashboardHandler, id, body string) (int, string) {
	t.Helper()
	req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/combos/"+id, strings.NewReader(body)), "id", id)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleUpdateCombo(rec, req)
	return rec.Code, rec.Body.String()
}

// The case that actually distinguishes the rename fix: the OLD name must be
// removed from the graph, because the pre-write graph still contains its edges
// while the post-write graph will not. Here the combo is renamed INTO a name an
// alias already points at, so if the old name's edges linger, the walk finds a
// loop that the write would in fact have destroyed — and a legitimate rename is
// falsely refused.
func TestRenameIntoAliasTargetDoesNotSeePhantomLoop(t *testing.T) {
	h, repo := newGraphTestHandler(t)

	// combo "old-name" -> ["cyc"], alias "cyc" -> "old-name": a real loop now.
	if err := repo.CreateCombo("c-phantom", "old-name", "fallback", `["cyc"]`, "fallback"); err != nil {
		t.Fatal(err)
	}
	valBytes, _ := json.Marshal("old-name")
	if err := repo.SetKV("modelAliases", "cyc", string(valBytes)); err != nil {
		t.Fatal(err)
	}

	// Rename the combo to "cyc" with a concrete leaf. After the write the loop
	// is gone: no combo named "old-name" exists, so the alias dangles. This must
	// be ALLOWED. Without removing the old name from the pre-write graph, the
	// walk would follow old-name -> cyc and report a loop that no longer exists.
	code, body := postComboUpdate(t, h, "c-phantom",
		`{"name":"cyc","kind":"fallback","models":["deepseek/deepseek-chat"]}`)
	if code != http.StatusOK {
		t.Fatalf("rename that destroys a loop was falsely refused: %d %s", code, body)
	}
}
