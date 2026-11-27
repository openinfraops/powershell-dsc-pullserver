package handlers

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

// setupIdentityAuditDB crée une base en mémoire avec les tables agents et audit,
// et configure global.AppConfig pour le driver sqlite (nécessaire à InsertAudit).
func setupIdentityAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	database := setupAgentsDB(t)
	// Avec ":memory:", chaque connexion du pool est une base VIDE distincte. On
	// force une connexion unique pour que les requêtes successives (SELECT de
	// l'ancienne empreinte puis des collisions) voient le même schéma.
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_email TEXT,
		action TEXT,
		target TEXT,
		details TEXT,
		ip_address TEXT,
		created_at TIMESTAMP
	)`); err != nil {
		t.Fatalf("create audit table: %v", err)
	}
	oldAppConfig := global.AppConfig
	global.AppConfig = &schema.AppConfig{Database: schema.DBConfig{Driver: "sqlite"}}
	t.Cleanup(func() { global.AppConfig = oldAppConfig })
	return database
}

func seedIdentityAgent(t *testing.T, database *sql.DB, agentID, nodeName, thumbprint, lastCommunication string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, last_communication, deleted_at) VALUES (?, ?, ?, ?, NULL)`,
		agentID, nodeName, thumbprint, lastCommunication); err != nil {
		t.Fatalf("seed agent %s: %v", agentID, err)
	}
}

func countAuditRows(t *testing.T, database *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatalf("count audit for %s: %v", action, err)
	}
	return n
}

func nowDBTime() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }
func staleDBTime() string {
	return time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
}

// TestDetectIdentityChange_NoCollision: un enregistrement d'un nœud nouveau, avec
// un nom libre, ne produit aucun audit d'écrasement ni de collision.
func TestDetectIdentityChange_NoCollision(t *testing.T) {
	database := setupIdentityAuditDB(t)
	detectAndAuditIdentityChange(database, "sqlite", "agent-new", "node-free", "AAA", "Initial", "10.0.0.1", true, "")
	if n := countAuditRows(t, database, "agent.overwrite"); n != 0 {
		t.Fatalf("expected 0 overwrite audits, got %d", n)
	}
	if n := countAuditRows(t, database, "agent.collision"); n != 0 {
		t.Fatalf("expected 0 collision audits, got %d", n)
	}
}

// TestDetectIdentityChange_FingerprintOverwrite: un agent existant (même
// agent_id) se ré-enregistre avec une autre empreinte => audit agent.overwrite
// renseignant l'ancienne et la nouvelle empreinte.
func TestDetectIdentityChange_FingerprintOverwrite(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "agent-1", "node-1", "AAA", nowDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-1", "node-1", "BBB", "Renewal", "10.0.0.1", false, "")

	if n := countAuditRows(t, database, "agent.overwrite"); n != 1 {
		t.Fatalf("expected 1 overwrite audit, got %d", n)
	}
	var details, ip string
	if err := database.QueryRow(`SELECT details, ip_address FROM audit WHERE action = 'agent.overwrite'`).Scan(&details, &ip); err != nil {
		t.Fatalf("query overwrite details: %v", err)
	}
	if !strings.Contains(details, "AAA -> BBB") || !strings.Contains(details, "node-1") {
		t.Fatalf("overwrite details missing info: %q", details)
	}
	if ip != "10.0.0.1" {
		t.Fatalf("expected overwrite ip 10.0.0.1, got %q", ip)
	}
}

// TestDetectIdentityChange_SameThumbprintNoOverwrite: un ré-enregistrement qui ne
// change pas d'empreinte ne doit pas être audité comme un écrasement.
func TestDetectIdentityChange_SameThumbprintNoOverwrite(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "agent-1", "node-1", "AAA", nowDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-1", "node-1", "AAA", "Renewal", "10.0.0.1", false, "")

	if n := countAuditRows(t, database, "agent.overwrite"); n != 0 {
		t.Fatalf("expected 0 overwrite audits for unchanged thumbprint, got %d", n)
	}
}

// TestDetectIdentityChange_NameCollisionLive: deux identités vivantes distinctes
// (autre agent_id, autre empreinte) revendiquent le même node_name dans la
// fenêtre => audit agent.collision.
func TestDetectIdentityChange_NameCollisionLive(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "agent-1", "node-x", "AAA", nowDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-2", "node-x", "BBB", "Initial", "10.0.0.2", true, "")

	if n := countAuditRows(t, database, "agent.collision"); n != 1 {
		t.Fatalf("expected 1 collision audit, got %d", n)
	}
	var details string
	if err := database.QueryRow(`SELECT details FROM audit WHERE action = 'agent.collision'`).Scan(&details); err != nil {
		t.Fatalf("query collision details: %v", err)
	}
	if !strings.Contains(details, "agent-1") || !strings.Contains(details, "agent-2") || !strings.Contains(details, "node-x") {
		t.Fatalf("collision details missing info: %q", details)
	}
}

// TestDetectIdentityChange_NameCollisionStale: un node_name détenu par une
// identité inactive depuis plus que la fenêtre n'est pas une collision (le
// remplacement est légitime) => aucun audit agent.collision.
func TestDetectIdentityChange_NameCollisionStale(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "agent-1", "node-x", "AAA", staleDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-2", "node-x", "BBB", "Initial", "10.0.0.2", true, "")

	if n := countAuditRows(t, database, "agent.collision"); n != 0 {
		t.Fatalf("expected 0 collision audits for stale identity, got %d", n)
	}
}

// TestDetectIdentityChange_SameIdentityReenroll: la même identité physique (même
// empreinte) qui se ré-enrôle sous un nouvel agent_id n'est pas une collision.
func TestDetectIdentityChange_SameIdentityReenroll(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "agent-1", "node-x", "AAA", nowDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-2", "node-x", "AAA", "Initial", "10.0.0.2", true, "")

	if n := countAuditRows(t, database, "agent.collision"); n != 0 {
		t.Fatalf("expected 0 collision audits for same identity, got %d", n)
	}
}

// TestDetectIdentityChange_TempExcluded: une pré-inscription TEMP du même nœud ne
// doit pas être considérée comme une collision de nom.
func TestDetectIdentityChange_TempExcluded(t *testing.T) {
	database := setupIdentityAuditDB(t)
	seedIdentityAgent(t, database, "TEMP-abc", "node-x", "", nowDBTime())
	detectAndAuditIdentityChange(database, "sqlite", "agent-2", "node-x", "BBB", "Initial", "10.0.0.2", true, "TEMP-abc")

	if n := countAuditRows(t, database, "agent.collision"); n != 0 {
		t.Fatalf("expected 0 collision audits for TEMP migration, got %d", n)
	}
}
