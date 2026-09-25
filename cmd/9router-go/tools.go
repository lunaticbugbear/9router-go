package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"9router/proxy/internal/db"
	"9router/proxy/internal/providers"
)

// schemaStatements is the canonical CREATE TABLE set, mirrored from
// internal/dbtest (the schema the Next.js dashboard shares). A fresh DATA_DIR
// gets only `upstream_leases`, which is why persona/bounty/apiKeys writes fail
// with "no such table" until the schema is created. Creating tables that
// already exist is a no-op, so this is safe to run on any install.
func schemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS apiKeys (
			id TEXT PRIMARY KEY,
			key TEXT UNIQUE NOT NULL,
			name TEXT,
			machineId TEXT,
			isActive INTEGER DEFAULT 1,
			createdAt TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS providerConnections (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			authType TEXT NOT NULL,
			name TEXT,
			email TEXT,
			priority INTEGER,
			isActive INTEGER DEFAULT 1,
			data TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kv (
			scope TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			PRIMARY KEY (scope, key)
		)`,
		`CREATE TABLE IF NOT EXISTS combos (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			kind TEXT,
			models TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			data TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS providerNodes (
			id TEXT PRIMARY KEY,
			type TEXT,
			name TEXT,
			data TEXT NOT NULL,
			createdAt TEXT NOT NULL,
			updatedAt TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS usageHistory (
			timestamp TEXT,
			provider TEXT,
			model TEXT,
			connectionId TEXT,
			apiKey TEXT,
			endpoint TEXT,
			promptTokens INTEGER,
			completionTokens INTEGER,
			cost REAL,
			status TEXT,
			tokens TEXT,
			meta TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS usageDaily (
			dateKey TEXT PRIMARY KEY,
			data TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS requestDetails (
			id TEXT PRIMARY KEY,
			timestamp TEXT,
			provider TEXT,
			model TEXT,
			connectionId TEXT,
			status TEXT,
			data TEXT
		)`,
	}
}

// openDBForCommand opens the gateway's SQLite file the same way the server
// does, without the server's lifecycle wiring.
func openDBForCommand() (*sql.DB, error) {
	path := os.Getenv("DB_PATH")
	if path == "" {
		path = resolveDataDir() + "/db/data.sqlite"
	}
	conn, err := db.OpenDatabase(path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return conn, nil
}

// doctorCheck names one diagnosable property of an install.
type doctorCheck struct {
	name string
	ok   bool
	note string
}

// runDoctor inspects the install offline: schema completeness and gateway
// reachability. It reports what it finds; it does not change anything.
func runDoctor(cCtx *cli.Context) error {
	checks := []doctorCheck{}

	conn, err := openDBForCommand()
	if err != nil {
		checks = append(checks, doctorCheck{"database", false, err.Error()})
	} else {
		defer conn.Close()
		missing, present, err := missingTables(conn)
		if err != nil {
			checks = append(checks, doctorCheck{"schema", false, err.Error()})
		} else {
			checks = append(checks, doctorCheck{"schema", len(missing) == 0,
				fmt.Sprintf("%d/%d tables present", len(present), len(present)+len(missing))})
			if len(missing) > 0 {
				checks[len(checks)-1].note += " (missing: " + strings.Join(missing, ", ") + ")"
			}
		}
	}

	port := cCtx.Int("port")
	if port == 0 {
		port = 20130
	}
	if waitForHealthy(cCtx.Context, port, 2*time.Second) {
		checks = append(checks, doctorCheck{"gateway", true, fmt.Sprintf("answering on :%d", port)})
	} else {
		checks = append(checks, doctorCheck{"gateway", false, fmt.Sprintf("no answer on :%d (may simply be stopped)", port)})
	}

	failed := 0
	for _, c := range checks {
		status := "ok"
		if !c.ok {
			status = "FAIL"
			failed++
		}
		fmt.Printf("%-6s %-10s %s\n", status, c.name, c.note)
	}
	if failed > 0 {
		return cli.Exit(fmt.Sprintf("%d check(s) failed", failed), 1)
	}
	fmt.Println("All checks passed.")
	return nil
}

// missingTables reports which canonical tables are absent in conn.
func missingTables(conn *sql.DB) (missing []string, present []string, err error) {
	for _, stmt := range schemaStatements() {
		name := tableName(stmt)
		var count int
		if err := conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count); err != nil {
			return nil, nil, fmt.Errorf("inspect table %s: %w", name, err)
		}
		if count == 0 {
			missing = append(missing, name)
		} else {
			present = append(present, name)
		}
	}
	return missing, present, nil
}

// tableName extracts the table name from a CREATE TABLE statement.
func tableName(stmt string) string {
	s := strings.TrimSpace(stmt)
	open := strings.Index(s, "(")
	head := s
	if open > 0 {
		head = s[:open]
	}
	fields := strings.Fields(head)
	if len(fields) >= 3 {
		return fields[len(fields)-1]
	}
	return ""
}

// runInitDB creates the canonical schema in the install's database. Idempotent:
// tables that already exist are left untouched, so it is safe to run on an
// existing install (it does not migrate or alter data).
func runInitDB(_ *cli.Context) error {
	conn, err := openDBForCommand()
	if err != nil {
		return err
	}
	defer conn.Close()

	created := 0
	for _, stmt := range schemaStatements() {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("create table %s: %w", tableName(stmt), err)
		}
		created++
	}
	fmt.Printf("Schema ready (%d tables verified).\n", created)
	fmt.Println("Existing rows, when present, are untouched.")
	return nil
}

// runModelsAudit explains where every catalog model's advertised context window
// came from. The resolver falls back to a bare guess when no rule matches, and
// the dashboard prints that guess exactly like a published figure, so an
// operator asking "is this really 200k?" has no way to tell without this.
//
// The exit code stays 0 because a guess is a fact about the catalog, not a
// command failure; --strict turns the count into a gate for scripts.
func runModelsAudit(cCtx *cli.Context) error {
	all, guessed := providers.AuditLimits()
	lines := providers.SummarizeLimits(all, guessed)
	if cCtx.Bool("verbose") {
		lines = append(lines, "", "Every model and the rule that decided it:")
		for _, limit := range all {
			source := limit.MatchedRule
			if source == "" {
				source = "FALLBACK GUESS"
			}
			lines = append(lines, fmt.Sprintf("  %-22s %-32s %8d  %s",
				limit.Provider, limit.Model, limit.ContextWindow, source))
		}
	}
	fmt.Println(strings.Join(lines, "\n"))
	if cCtx.Bool("strict") && len(guessed) > 0 {
		return cli.Exit("", 1)
	}
	return nil
}
