package db

import (
	"database/sql"
	"testing"
)

func agentTrashTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE agents (agent_id TEXT PRIMARY KEY, node_name TEXT, needs_recompile INTEGER DEFAULT 0, deleted_at TIMESTAMP NULL, deleted_by TEXT NULL)`,
		`CREATE TABLE agent_tags (agent_id TEXT, tag_key TEXT, tag_value TEXT)`,
		`CREATE TABLE reports (agent_id TEXT, created_at TIMESTAMP)`,
		`CREATE TABLE agent_configurations (agent_id TEXT, configuration_name TEXT)`,
		`CREATE TABLE agent_ips (agent_id TEXT, ip TEXT)`,
		`CREATE TABLE node_properties (node_id TEXT, property_id INTEGER, value TEXT)`,
		`INSERT INTO agents (agent_id, node_name) VALUES ('old-1', 'old-node')`,
		`INSERT INTO agents (agent_id, node_name) VALUES ('fresh-1', 'fresh-node')`,
		`INSERT INTO agent_tags (agent_id, tag_key, tag_value) VALUES ('old-1', 'env', 'prod')`,
		`INSERT INTO reports (agent_id, created_at) VALUES ('old-1', CURRENT_TIMESTAMP)`,
		`INSERT INTO agent_configurations (agent_id, configuration_name) VALUES ('old-1', 'cfg')`,
		`INSERT INTO agent_ips (agent_id, ip) VALUES ('old-1', '1.2.3.4')`,
		`INSERT INTO node_properties (node_id, property_id, value) VALUES ('old-1', 1, 'v')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema setup: %v", err)
		}
	}
}

func TestSoftDeleteAndRestoreAgent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	agentTrashTestSchema(t, db)

	if err := SoftDeleteAgent(db, "sqlite", "fresh-1", "admin@example.com"); err != nil {
		t.Fatalf("SoftDeleteAgent: %v", err)
	}
	var deletedAt, deletedBy sql.NullString
	if err := db.QueryRow(`SELECT deleted_at, deleted_by FROM agents WHERE agent_id = 'fresh-1'`).Scan(&deletedAt, &deletedBy); err != nil {
		t.Fatal(err)
	}
	if !deletedAt.Valid {
		t.Fatalf("expected deleted_at to be set after soft delete")
	}
	if deletedBy.String != "admin@example.com" {
		t.Fatalf("expected deleted_by=admin@example.com, got %q", deletedBy.String)
	}

	if err := RestoreAgent(db, "sqlite", "fresh-1"); err != nil {
		t.Fatalf("RestoreAgent: %v", err)
	}
	var after sql.NullString
	if err := db.QueryRow(`SELECT deleted_at FROM agents WHERE agent_id = 'fresh-1'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after.Valid {
		t.Fatalf("expected deleted_at to be NULL after restore, got %v", after)
	}
}

func TestPurgeDeletedAgentsCascadesAndKeepsFresh(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	agentTrashTestSchema(t, db)

	// old-1 est en corbeille depuis 10 jours ; fresh-1 est actif.
	if _, err := db.Exec(`UPDATE agents SET deleted_at = datetime('now', '-10 days') WHERE agent_id = 'old-1'`); err != nil {
		t.Fatal(err)
	}

	purged, err := PurgeDeletedAgents(db, "sqlite", 5)
	if err != nil {
		t.Fatalf("PurgeDeletedAgents: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged agent, got %d", purged)
	}

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = 'old-1'`).Scan(&count)
	if count != 0 {
		t.Fatalf("old-1 should be purged")
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM agent_tags WHERE agent_id = 'old-1'`).Scan(&count)
	if count != 0 {
		t.Fatalf("old-1 tags should be purged")
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM reports WHERE agent_id = 'old-1'`).Scan(&count)
	if count != 0 {
		t.Fatalf("old-1 reports should be purged")
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM agent_configurations WHERE agent_id = 'old-1'`).Scan(&count)
	if count != 0 {
		t.Fatalf("old-1 configurations should be purged")
	}

	// Le nœud actif est conservé.
	_ = db.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = 'fresh-1'`).Scan(&count)
	if count != 1 {
		t.Fatalf("fresh-1 should remain")
	}
}
