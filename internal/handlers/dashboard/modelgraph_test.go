package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"9router/proxy/internal/db"
)

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
