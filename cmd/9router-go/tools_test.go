package main

import (
	"path/filepath"
	"testing"

	"9router/proxy/internal/db"
)

func TestInitDBCreatesProxyPoolsSchema(t *testing.T) {
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "data.sqlite"))

	if err := runInitDB(nil); err != nil {
		t.Fatalf("runInitDB() error = %v", err)
	}
	if err := runInitDB(nil); err != nil {
		t.Fatalf("second runInitDB() error = %v", err)
	}

	conn, err := openDBForCommand()
	if err != nil {
		t.Fatalf("open initialized database: %v", err)
	}
	defer conn.Close()

	pools, err := db.NewRepo(conn).ListProxyPools()
	if err != nil {
		t.Fatalf("ListProxyPools() after init-db: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("expected empty proxy pool list, got %d entries", len(pools))
	}
}
