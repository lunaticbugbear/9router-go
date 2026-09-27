package dashboard

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProxyPools_EmptyList(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	router := setupTestRouter(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/proxy-pools?includeUsage=true", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET proxy pools expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		ProxyPools []map[string]any `json:"proxyPools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode proxy pool response: %v", err)
	}
	if len(response.ProxyPools) != 0 {
		t.Fatalf("expected empty proxy pool list, got %d entries", len(response.ProxyPools))
	}
}

// Upstream parity: deleting a pool with bound connections must fail with 409
// and report boundConnectionCount; unbound pools delete with 200.
func TestDeleteProxyPoolBoundConflict(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := repo.RawDB().Exec(`CREATE TABLE IF NOT EXISTS proxyPools (
		id TEXT PRIMARY KEY,
		isActive INTEGER DEFAULT 1,
		testStatus TEXT,
		data TEXT NOT NULL,
		createdAt TEXT NOT NULL,
		updatedAt TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("failed to create proxyPools table: %v", err)
	}

	router := setupTestRouter(repo)

	post := func(path string, body map[string]any) *httptest.ResponseRecorder {
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Create a pool.
	rec := post("/api/proxy-pools", map[string]any{
		"name":     "Test Pool",
		"proxyUrl": "http://127.0.0.1:7890",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var pool map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pool); err != nil {
		t.Fatalf("failed to unmarshal pool: %v", err)
	}
	poolID, _ := pool["id"].(string)
	if poolID == "" {
		t.Fatalf("created pool has no id: %v", pool)
	}

	// 2. Create a connection bound to the pool.
	rec = post("/api/connections", map[string]any{
		"provider":    "openai",
		"authType":    "apikey",
		"name":        "Bound Key",
		"apiKey":      "sk-test-123",
		"proxyPoolId": poolID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create connection expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var conn map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &conn); err != nil {
		t.Fatalf("failed to unmarshal connection: %v", err)
	}
	connID, _ := conn["id"].(string)

	// 3. Delete the bound pool → 409 with boundConnectionCount = 1.
	req := httptest.NewRequest(http.MethodDelete, "/api/proxy-pools/"+poolID, nil)
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, req)
	if delRec.Code != http.StatusConflict {
		t.Fatalf("delete bound pool expected 409, got %d: %s", delRec.Code, delRec.Body.String())
	}
	var conflict map[string]any
	if err := json.Unmarshal(delRec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("failed to unmarshal 409 body: %v", err)
	}
	if conflict["boundConnectionCount"] != float64(1) {
		t.Fatalf("expected boundConnectionCount 1, got %v", conflict)
	}

	// 4. Delete the connection, then the pool → 200.
	req = httptest.NewRequest(http.MethodDelete, "/api/connections/"+connID, nil)
	delRec = httptest.NewRecorder()
	router.ServeHTTP(delRec, req)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete connection expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/proxy-pools/"+poolID, nil)
	delRec = httptest.NewRecorder()
	router.ServeHTTP(delRec, req)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete unbound pool expected 200, got %d: %s", delRec.Code, delRec.Body.String())
	}
}
