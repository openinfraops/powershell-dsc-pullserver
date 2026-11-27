package db

import (
	"database/sql"
	"strings"
	"testing"
)

func newIndexMaintenanceDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		node_name TEXT,
		certificate_thumbprint TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE INDEX idx_agents_certificate_thumbprint ON agents(certificate_thumbprint)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES ('a','n','906bf4b01d9a6916ce2848e94b7aaa825d9a1ba0')`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestRunIndexMaintenanceSQLite(t *testing.T) {
	database := newIndexMaintenanceDB(t)
	msg, err := RunIndexMaintenance(database, "sqlite", false)
	if err != nil {
		t.Fatalf("RunIndexMaintenance: %v", err)
	}
	if !strings.Contains(msg, "REINDEX") {
		t.Fatalf("expected REINDEX in message, got %q", msg)
	}
	// La table doit toujours être interrogeable après maintenance.
	if _, err := database.Query(`SELECT COUNT(*) FROM agents`); err != nil {
		t.Fatalf("query after maintenance: %v", err)
	}
}

func TestRunIndexMaintenanceSQLiteVacuum(t *testing.T) {
	database := newIndexMaintenanceDB(t)
	msg, err := RunIndexMaintenance(database, "sqlite", true)
	if err != nil {
		t.Fatalf("RunIndexMaintenance vacuum: %v", err)
	}
	if !strings.Contains(msg, "VACUUM") {
		t.Fatalf("expected VACUUM in message, got %q", msg)
	}
}

func TestRunIndexMaintenanceMSSQLUnsupportedDriverIsTreatedAsSQLite(t *testing.T) {
	// Le driver par défaut (tout ce qui n'est pas mssql/sqlserver) est traité comme
	// SQLite : vérifie qu'un driver inconnu aboutit au chemin SQLite sans erreur.
	database := newIndexMaintenanceDB(t)
	if _, err := RunIndexMaintenance(database, "mariadb", false); err != nil {
		t.Fatalf("unknown driver should fall back to sqlite path: %v", err)
	}
}
