package handlers

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// setupAgentsDB crée une base SQLite en mémoire avec la table agents (et la
// table agent_configurations référencée par la clé étrangère).
func setupAgentsDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	_, err = database.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		node_name TEXT,
		lcm_version TEXT,
		registration_type TEXT,
		certificate_thumbprint TEXT,
		certificate_subject TEXT,
		certificate_issuer TEXT,
		certificate_notbefore TEXT,
		certificate_notafter TEXT,
		certificate_public_key TEXT,
		certificate_key_algorithm TEXT,
		registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_communication TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		has_error_last_report BOOLEAN DEFAULT 0,
		needs_recompile INTEGER DEFAULT 0,
		force_cert_rotation INTEGER DEFAULT 0,
		deleted_at TIMESTAMP NULL,
		deleted_by TEXT NULL,
		state TEXT
	)`)
	if err != nil {
		t.Fatalf("create agents table: %v", err)
	}
	return database
}

// TestUpsertAgent_NewNode vérifie qu'un nouvel enregistrement (enrôlement)
// insère l'agent avec un état 'pending_apply' et le certificat fourni.
func TestUpsertAgent_NewNode(t *testing.T) {
	database := setupAgentsDB(t)
	err := upsertAgent(database, "sqlite", "agent-1", "node-1", "1.0", "Initial", "AAA", "subject", "issuer", "2024-01-01", "2030-01-01", "", "", true)
	if err != nil {
		t.Fatalf("upsertAgent new node: %v", err)
	}

	var state, thumbprint string
	err = database.QueryRow(`SELECT state, certificate_thumbprint FROM agents WHERE agent_id = ?`, "agent-1").Scan(&state, &thumbprint)
	if err != nil {
		t.Fatalf("query new node: %v", err)
	}
	if state != "pending_apply" {
		t.Fatalf("expected state 'pending_apply', got %q", state)
	}
	if thumbprint != "AAA" {
		t.Fatalf("expected thumbprint 'AAA', got %q", thumbprint)
	}
}

// TestUpsertAgent_Renewal vérifie qu'un ré-enregistrement d'un agent existant
// (renouvellement de certificat) rafraîchit l'empreinte SANS écraser l'état,
// last_communication, has_error_last_report ni needs_recompile.
func TestUpsertAgent_Renewal(t *testing.T) {
	database := setupAgentsDB(t)
	// Agent existant, avec un ancien certificat et un état déjà en place.
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, state, last_communication, has_error_last_report, needs_recompile) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"agent-1", "node-1", "1.0", "Initial", "OLDTHUMB", "subject", "issuer", "2020-01-01", "2025-01-01", "compliant", "2025-06-01 00:00:00", true, 1); err != nil {
		t.Fatalf("seed existing agent: %v", err)
	}

	// Ré-enregistrement avec un NOUVEAU certificat (renouvellement).
	err := upsertAgent(database, "sqlite", "agent-1", "node-1", "2.0", "Renewal", "NEWTHUMB", "subject2", "issuer2", "2026-01-01", "2031-01-01", "PEMKEY", "RSA", false)
	if err != nil {
		t.Fatalf("upsertAgent renewal: %v", err)
	}

	var thumbprint, state, lastComm string
	var hasError, needsRecompile bool
	err = database.QueryRow(`SELECT certificate_thumbprint, state, last_communication, has_error_last_report, needs_recompile FROM agents WHERE agent_id = ?`, "agent-1").Scan(&thumbprint, &state, &lastComm, &hasError, &needsRecompile)
	if err != nil {
		t.Fatalf("query after renewal: %v", err)
	}
	if thumbprint != "NEWTHUMB" {
		t.Fatalf("expected refreshed thumbprint 'NEWTHUMB', got %q", thumbprint)
	}
	if state != "compliant" {
		t.Fatalf("expected state preserved ('compliant'), got %q", state)
	}
	if !hasError {
		t.Fatalf("expected has_error_last_report preserved (true)")
	}
	if !needsRecompile {
		t.Fatalf("expected needs_recompile preserved (true)")
	}
	if !strings.Contains(lastComm, "2025-06-01") {
		t.Fatalf("expected last_communication preserved, got %q", lastComm)
	}
}

// TestUpsertAgent_Renewal_UnknownAgent vérifie qu'un UPDATE sur un agent
// inexistant ne provoque pas d'erreur (0 ligne affectée), sans régression.
func TestUpsertAgent_Renewal_UnknownAgent(t *testing.T) {
	database := setupAgentsDB(t)
	err := upsertAgent(database, "sqlite", "ghost", "node-x", "1.0", "Initial", "THUMB", "subject", "issuer", "2024-01-01", "2030-01-01", "", "", false)
	if err != nil {
		t.Fatalf("upsertAgent on missing agent should not error, got: %v", err)
	}
}

// TestUpsertAgent_Renewal_ClearsForceRotation vérifie qu'un ré-enregistrement
// (renouvellement de certificat) lève le drapeau force_cert_rotation : le nœud
// a présenté un nouveau certificat, la demande « ASAP » d'un admin est satisfaite.
func TestUpsertAgent_Renewal_ClearsForceRotation(t *testing.T) {
	database := setupAgentsDB(t)
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state, force_cert_rotation) VALUES (?, ?, ?, ?, ?)`,
		"agent-1", "node-1", "OLDTHUMB", "compliant", 1); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	if err := upsertAgent(database, "sqlite", "agent-1", "node-1", "2.0", "Renewal", "NEWTHUMB", "subject2", "issuer2", "2026-01-01", "2031-01-01", "PEMKEY", "RSA", false); err != nil {
		t.Fatalf("upsertAgent renewal: %v", err)
	}

	var force int
	if err := database.QueryRow(`SELECT force_cert_rotation FROM agents WHERE agent_id = ?`, "agent-1").Scan(&force); err != nil {
		t.Fatalf("read force_cert_rotation: %v", err)
	}
	if force != 0 {
		t.Fatalf("expected force_cert_rotation cleared after re-registration, got %d", force)
	}
}
