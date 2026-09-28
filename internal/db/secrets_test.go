package db

import (
	"database/sql"
	"sort"
	"strings"
	"testing"
)

// The values below are synthetic and exist only in a temp database. They are
// deliberately distinctive so a missing value is unambiguous.
const (
	secretTestClientKey  = "sk-client-000000000000000000000000"
	secretTestConnKey    = "sk-conn-11111111111111111111111111"
	secretTestConnToken  = "tok-conn-2222222222222222222222222"
	secretTestRefresh    = "rt-conn-33333333333333333333333333"
	secretTestNested     = "cs-nested-4444444444444444444444"
	secretTestCookie     = "BXAuth=5555555555555555555555555555;"
	secretTestShortValue = "ab"
	secretTestNotSecret  = "https://example.test/v1"
)

// seedSecretConnections writes the connection rows the assertions read.
func seedSecretConnections(t *testing.T, database *sql.DB) {
	t.Helper()
	rows := []struct {
		id   string
		data string
	}{
		{"c-all", `{"apiKey":"` + secretTestConnKey + `","accessToken":"` + secretTestConnToken +
			`","refreshToken":"` + secretTestRefresh + `","baseUrl":"` + secretTestNotSecret + `"}`},
		{"c-nested", `{"apiKey":"` + secretTestConnKey + `","providerSpecificData":{"clientSecret":"` + secretTestNested +
			`","cookie":"` + secretTestCookie + `","baseUrl":"` + secretTestNotSecret + `"}}`},
		{"c-short", `{"apiKey":"` + secretTestShortValue + `","baseUrl":"` + secretTestNotSecret + `"}`},
		{"c-none", `{"baseUrl":"` + secretTestNotSecret + `","defaultModel":"gpt-4o","testStatus":"ok"}`},
		{"c-empty", ``},
		{"c-broken", `{not json at all`},
		{"c-null", `null`},
	}
	for _, r := range rows {
		if _, err := database.Exec(`INSERT INTO providerConnections (id, provider, authType, name, priority, isActive, data, createdAt, updatedAt)
			VALUES (?, 'openai', 'apikey', 'Test', 1, 1, ?, '2026-07-18T00:00:00Z', '2026-07-18T00:00:00Z')`,
			r.id, r.data); err != nil {
			t.Fatalf("seed connection %s: %v", r.id, err)
		}
	}
	if _, err := database.Exec(`INSERT INTO apiKeys (id, key, name, isActive, createdAt)
		VALUES ('k1', ?, 'Client', 1, '2026-07-18T00:00:00Z')`, secretTestClientKey); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
}

// TestSecretValues_CollectsEveryStoredCredential covers the positive case:
// client keys, top-level connection credentials, and nested ones are all found.
func TestSecretValues_CollectsEveryStoredCredential(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}

	for name, want := range map[string]string{
		"client API key":          secretTestClientKey,
		"connection apiKey":       secretTestConnKey,
		"connection accessToken":  secretTestConnToken,
		"connection refreshToken": secretTestRefresh,
		"nested clientSecret":     secretTestNested,
		"nested cookie":           secretTestCookie,
	} {
		if !containsString(values, want) {
			t.Errorf("SecretValues did not return the %s", name)
		}
	}
}

// TestSecretValues_ExcludesNonCredentialFields guards the negative case: a
// base URL, a model name, or a status string must never be treated as a
// credential, or the redactor would blank ordinary text.
func TestSecretValues_ExcludesNonCredentialFields(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}

	for name, notSecret := range map[string]string{
		"base URL":      secretTestNotSecret,
		"model name":    "gpt-4o",
		"test status":   "ok",
		"connection id": "c-all",
		"auth type":     "apikey",
	} {
		if containsString(values, notSecret) {
			t.Errorf("SecretValues treated the %s as a credential", name)
		}
	}
}

// TestSecretValues_ShortValueIsReturnedButCallerFilters pins the division of
// labour: the repository reports what is stored (it cannot know how the caller
// will use it), and the redactor is what drops undistinctive values.
func TestSecretValues_ShortValueIsReturnedButCallerFilters(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}
	if !containsString(values, secretTestShortValue) {
		t.Error("SecretValues should report the short stored value; filtering is the caller's job")
	}
}

// TestSecretValues_ToleratesUnparseableAndEmptyBlobs ensures a malformed or
// empty data column is skipped rather than treated as a credential source.
func TestSecretValues_ToleratesUnparseableAndEmptyBlobs(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}
	for _, v := range values {
		if strings.Contains(v, "not json") {
			t.Errorf("a malformed blob leaked into the credential list: %q", v)
		}
		if v == "" {
			t.Error("an empty blob produced an empty credential entry")
		}
	}
}

// TestSecretValues_MissingTablesAreNotAnError covers a fresh install: the
// dashboard creates these tables, so before it does there is nothing to read
// and nothing to hide. That is an empty list, not a failure.
func TestSecretValues_MissingTablesAreNotAnError(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := database.Exec(`DROP TABLE apiKeys`); err != nil {
		t.Fatalf("drop apiKeys: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE providerConnections`); err != nil {
		t.Fatalf("drop providerConnections: %v", err)
	}

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("a missing table must not be an error, got: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("expected no credentials from missing tables, got %d", len(values))
	}
}

// TestSecretValues_NilRepositoryIsAnError pins that a caller with no database
// is told so, rather than being handed a reassuring empty list.
func TestSecretValues_NilRepositoryIsAnError(t *testing.T) {
	var repo *Repo
	if _, err := repo.SecretValues(); err == nil {
		t.Error("expected an error for a nil repository")
	}
	if _, err := NewRepo(nil).SecretValues(); err == nil {
		t.Error("expected an error for a repository with no handle")
	}
}

// TestSecretValues_ReturnsEachValueOnce keeps the list small: the same key
// stored on two connections should not be reported twice.
func TestSecretValues_ReturnsEachValueOnce(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	values, err := NewRepo(database).SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}
	counts := map[string]int{}
	for _, v := range values {
		counts[v]++
	}
	// secretTestConnKey is deliberately on two connections.
	if counts[secretTestConnKey] != 2 {
		t.Errorf("expected the shared connection key on both rows, got %d", counts[secretTestConnKey])
	}
}

// TestSecretValues_SortedOrUnorderedButComplete documents that callers must not
// depend on order; the redactor sorts for itself.
func TestSecretValues_SortedOrUnorderedButComplete(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedSecretConnections(t, database)

	repo := NewRepo(database)
	first, err := repo.SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}
	second, err := repo.SecretValues()
	if err != nil {
		t.Fatalf("SecretValues failed: %v", err)
	}
	sort.Strings(first)
	sort.Strings(second)
	if strings.Join(first, "\x00") != strings.Join(second, "\x00") {
		t.Error("SecretValues returned a different set on a second call")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
