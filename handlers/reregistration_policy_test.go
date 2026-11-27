package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"

	_ "modernc.org/sqlite"
)

// setupReregDB crée une base SQLite en mémoire avec les tables agents
// (incluant persistent et reregistration_window_until), dsc_settings et audit,
// et configure global.AppConfig (pilote sqlite + clé d'enregistrement DSC).
func setupReregDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Avec ":memory:", chaque connexion du pool est une base VIDE distincte. On
	// force une connexion unique pour que les requêtes successives voient le
	// même schéma et les mêmes données.
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		database.Close()
		db.SetShared(nil)
	})

	for _, stmt := range []string{
		`CREATE TABLE agents (
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
			state TEXT,
			reboot_pending INTEGER DEFAULT 0,
			reboot_pending_since TIMESTAMP NULL,
			reboot_requested_by TEXT NULL,
			reboot_loop INTEGER DEFAULT 0,
			reboot_request_count INTEGER DEFAULT 0,
			persistent INTEGER DEFAULT 0,
			reregistration_window_until TIMESTAMP NULL
		)`,
		`CREATE TABLE agent_configurations (
			agent_id TEXT,
			configuration_name TEXT,
			PRIMARY KEY (agent_id, configuration_name)
		)`,
		`CREATE TABLE dsc_settings (
			id INTEGER PRIMARY KEY,
			certificate_rotation_enabled INTEGER DEFAULT 0,
			certificate_renewal_days INTEGER DEFAULT 30,
			legacy_imported INTEGER DEFAULT 0,
			reregistration_mode TEXT DEFAULT 'deny',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT,
			action TEXT,
			target TEXT,
			details TEXT,
			ip_address TEXT,
			created_at TIMESTAMP
		)`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	if _, err := database.Exec(`INSERT OR IGNORE INTO dsc_settings (id, certificate_rotation_enabled, certificate_renewal_days, legacy_imported, reregistration_mode) VALUES (1, 0, 30, 1, 'deny')`); err != nil {
		t.Fatalf("seed dsc_settings: %v", err)
	}

	old := global.AppConfig
	global.AppConfig = &schema.AppConfig{
		Database:      schema.DBConfig{Driver: "sqlite"},
		DSCPullServer: schema.HTTPConfig{RegistrationKey: "test-registration-key"},
		WebUI:         schema.HTTPConfig{TrustedProxies: []string{}},
	}
	t.Cleanup(func() { global.AppConfig = old })
	db.SetShared(database)
	return database
}

// setReregMode force le mode de la politique de ré-enregistrement (#87).
func setReregMode(t *testing.T, database *sql.DB, mode string) {
	t.Helper()
	if _, err := database.Exec(`UPDATE dsc_settings SET reregistration_mode = ? WHERE id = 1`, mode); err != nil {
		t.Fatalf("set reregistration_mode=%s: %v", mode, err)
	}
}

// seedPersistentAgent insère un nœud (optionnellement persistant) avec une
// ancienne empreinte, un état et une éventuelle borne de fenêtre.
func seedPersistentAgent(t *testing.T, database *sql.DB, agentID, nodeName, thumbprint, state string, persistent bool, windowUntil interface{}) {
	t.Helper()
	persist := 0
	if persistent {
		persist = 1
	}
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state, persistent, reregistration_window_until, deleted_at) VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		agentID, nodeName, thumbprint, state, persist, windowUntil); err != nil {
		t.Fatalf("seed agent %s: %v", agentID, err)
	}
}

func countAuditAction(t *testing.T, database *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatalf("count audit %s: %v", action, err)
	}
	return n
}

func lastAuditDetails(t *testing.T, database *sql.DB, action string) string {
	t.Helper()
	var details string
	if err := database.QueryRow(`SELECT details FROM audit WHERE action = ? ORDER BY id DESC LIMIT 1`, action).Scan(&details); err != nil {
		t.Fatalf("read audit %s: %v", action, err)
	}
	return details
}

// --- Unit tests du verdict de politique (applyReregistrationPolicy) ---

func TestReregPolicy_DefaultDeny(t *testing.T) {
	database := setupReregDB(t)
	// Mode par défaut = deny (rangée dsc_settings initiale).
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregReject {
		t.Fatalf("expected reject in deny mode, got %v", dec.action)
	}
	if dec.mode != "deny" || dec.outcome != "denied" {
		t.Fatalf("expected mode=deny outcome=denied, got %s/%s", dec.mode, dec.outcome)
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 1 {
		t.Fatalf("expected 1 reregistration audit, got %d", n)
	}
	details := lastAuditDetails(t, database, "agent.reregistration")
	if !strings.Contains(details, "OLD -> NEW") || !strings.Contains(details, "mode=deny") || !strings.Contains(details, "outcome=denied") {
		t.Fatalf("audit details missing info: %q", details)
	}
}

func TestReregPolicy_NonPersistent(t *testing.T) {
	database := setupReregDB(t)
	// Nœud non persistant : la politique ne s'applique pas.
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", false, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregAllow {
		t.Fatalf("expected allow for non-persistent node, got %v", dec.action)
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 0 {
		t.Fatalf("expected 0 reregistration audits for non-persistent node, got %d", n)
	}
}

func TestReregPolicy_NewNodeNotGated(t *testing.T) {
	database := setupReregDB(t)
	// Nouvel enrôlement (agent_id inconnu) : pas de politique.
	dec := applyReregistrationPolicy(database, "sqlite", "agent-new", "node-new", "THUMB", "10.0.0.1", true, "")
	if dec.action != reregAllow {
		t.Fatalf("expected allow for new node, got %v", dec.action)
	}
}

func TestReregPolicy_TempMigrationNotGated(t *testing.T) {
	database := setupReregDB(t)
	seedPersistentAgent(t, database, "TEMP-abc", "node-1", "", "waiting_for_registration", false, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "THUMB", "10.0.0.1", false, "TEMP-abc")
	if dec.action != reregAllow {
		t.Fatalf("expected allow for TEMP migration, got %v", dec.action)
	}
}

func TestReregPolicy_AllowMode(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "allow")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregAllow {
		t.Fatalf("expected allow in allow mode, got %v", dec.action)
	}
	if dec.outcome != "allowed" {
		t.Fatalf("expected outcome=allowed, got %s", dec.outcome)
	}
}

func TestReregPolicy_ApprovalMode(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "approval")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregPending {
		t.Fatalf("expected pending in approval mode, got %v", dec.action)
	}
	if dec.outcome != "pending" {
		t.Fatalf("expected outcome=pending, got %s", dec.outcome)
	}
}

func TestReregPolicy_ProofMatchingThumbprint(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "proof")
	seedPersistentAgent(t, database, "agent-1", "node-1", "AAA", "compliant", true, nil)

	// La même empreinte (la clé précédemment enrôlée) est acceptée.
	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "AAA", "10.0.0.1", false, "")
	if dec.action != reregAllow {
		t.Fatalf("expected allow in proof mode with matching thumbprint, got %v", dec.action)
	}
	if dec.outcome != "proof_ok" {
		t.Fatalf("expected outcome=proof_ok, got %s", dec.outcome)
	}
}

func TestReregPolicy_ProofDifferentThumbprint(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "proof")
	seedPersistentAgent(t, database, "agent-1", "node-1", "AAA", "compliant", true, nil)

	// Une autre empreinte n'est pas une preuve : rejet.
	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "BBB", "10.0.0.1", false, "")
	if dec.action != reregReject {
		t.Fatalf("expected reject in proof mode with different thumbprint, got %v", dec.action)
	}
	if dec.outcome != "proof_failed" {
		t.Fatalf("expected outcome=proof_failed, got %s", dec.outcome)
	}
}

func TestReregPolicy_WindowOpen(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "window")
	future := time.Now().UTC().Add(4 * time.Hour).Format("2006-01-02 15:04:05")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, future)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregAllow {
		t.Fatalf("expected allow in window mode with open window, got %v", dec.action)
	}
	if dec.outcome != "window_open" {
		t.Fatalf("expected outcome=window_open, got %s", dec.outcome)
	}
}

func TestReregPolicy_WindowClosed(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "window")
	past := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, past)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregReject {
		t.Fatalf("expected reject in window mode with closed window, got %v", dec.action)
	}
	if dec.outcome != "window_closed" {
		t.Fatalf("expected outcome=window_closed, got %s", dec.outcome)
	}
}

func TestReregPolicy_WindowNoWindowSet(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "window")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	dec := applyReregistrationPolicy(database, "sqlite", "agent-1", "node-1", "NEW", "10.0.0.1", false, "")
	if dec.action != reregReject {
		t.Fatalf("expected reject in window mode with no window, got %v", dec.action)
	}
	if dec.outcome != "window_closed" {
		t.Fatalf("expected outcome=window_closed, got %s", dec.outcome)
	}
}

// --- Intégration : RegisterHandler applique la politique (#87) ---

func signDSCBody(t *testing.T, body []byte, xmsDate, key string) string {
	t.Helper()
	h := sha256.Sum256(body)
	bodyHashB64 := base64.StdEncoding.EncodeToString(h[:])
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(bodyHashB64 + "\n" + xmsDate))
	return "Shared " + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func newRegisterRequest(t *testing.T, agentID string, body []byte, registrationKey string) *http.Request {
	t.Helper()
	xmsDate := "2025-01-01T00:00:00Z"
	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes", bytes.NewReader(body))
	req.SetPathValue("node", agentID)
	req.Header.Set("x-ms-date", xmsDate)
	req.Header.Set("Authorization", signDSCBody(t, body, xmsDate, registrationKey))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func reregPayload(nodeName, thumbprint string) []byte {
	payload := map[string]interface{}{
		"AgentInformation": map[string]interface{}{
			"NodeName":   nodeName,
			"LCMVersion": "1.0",
		},
		"RegistrationInformation": map[string]interface{}{
			"RegistrationMessageType": "Renewal",
			"CertificateInformation": map[string]interface{}{
				"Thumbprint": thumbprint,
				"Subject":    "subject",
				"Issuer":     "issuer",
				"NotBefore":  "2026-01-01",
				"NotAfter":   "2031-01-01",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestRegisterHandler_DenyRejectsPersistent(t *testing.T) {
	database := setupReregDB(t)
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	// Mode deny (défaut) : un nœud persistant ne peut pas se ré-enregistrer.
	body := reregPayload("node-1", "NEW")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on deny re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 1 {
		t.Fatalf("expected 1 reregistration audit, got %d", n)
	}
}

func TestRegisterHandler_ApprovalSetsPending(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "approval")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	body := reregPayload("node-1", "NEW")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on approval re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
	var state string
	if err := database.QueryRow(`SELECT state FROM agents WHERE agent_id = ?`, "agent-1").Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "pending_reregistration" {
		t.Fatalf("expected state pending_reregistration, got %q", state)
	}
	var thumb string
	_ = database.QueryRow(`SELECT certificate_thumbprint FROM agents WHERE agent_id = ?`, "agent-1").Scan(&thumb)
	// RegisterHandler normalise l'empreinte en minuscules.
	if thumb != "new" {
		t.Fatalf("expected thumbprint refreshed to new, got %q", thumb)
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 1 {
		t.Fatalf("expected 1 reregistration audit, got %d", n)
	}
}

func TestRegisterHandler_ProofMatchingAccepts(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "proof")
	seedPersistentAgent(t, database, "agent-1", "node-1", "AAA", "compliant", true, nil)

	body := reregPayload("node-1", "AAA")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on proof-matched re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 1 {
		t.Fatalf("expected 1 reregistration audit, got %d", n)
	}
	if !strings.Contains(lastAuditDetails(t, database, "agent.reregistration"), "proof_ok") {
		t.Fatalf("expected proof_ok outcome, got %q", lastAuditDetails(t, database, "agent.reregistration"))
	}
}

func TestRegisterHandler_ProofDifferentRejects(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "proof")
	seedPersistentAgent(t, database, "agent-1", "node-1", "AAA", "compliant", true, nil)

	body := reregPayload("node-1", "BBB")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on proof-mismatch, got %d (body=%s)", w.Code, w.Body.String())
	}
	if n := countAuditAction(t, database, "agent.reregistration"); n != 1 {
		t.Fatalf("expected 1 reregistration audit, got %d", n)
	}
}

func TestRegisterHandler_WindowOpenAccepts(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "window")
	future := time.Now().UTC().Add(4 * time.Hour).Format("2006-01-02 15:04:05")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, future)

	body := reregPayload("node-1", "NEW")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on open window re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(lastAuditDetails(t, database, "agent.reregistration"), "window_open") {
		t.Fatalf("expected window_open outcome, got %q", lastAuditDetails(t, database, "agent.reregistration"))
	}
}

func TestRegisterHandler_WindowClosedRejects(t *testing.T) {
	database := setupReregDB(t)
	setReregMode(t, database, "window")
	past := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, past)

	body := reregPayload("node-1", "NEW")
	w := httptest.NewRecorder()
	RegisterHandler(w, newRegisterRequest(t, "agent-1", body, "test-registration-key"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on closed window, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestGetDscAction_PendingReregBlocksConfig vérifie qu'un nœud en
// 'pending_reregistration' ne reçoit pas sa configuration (#87).
func TestGetDscAction_PendingReregBlocksConfig(t *testing.T) {
	database := setupReregDB(t)
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "pending_reregistration", true, nil)
	if _, err := database.Exec(`INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, "agent-1", "node-1"); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/agent-1/GetDscAction", strings.NewReader(`{}`))
	req.SetPathValue("node", "agent-1")
	w := httptest.NewRecorder()
	GetDscActionNodeHandlerWithId(w, req, "agent-1")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (configuration not served) for pending re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// --- Intégration : API admin (#87) ---

func TestAdminSetPersistentHandler(t *testing.T) {
	database := setupReregDB(t)
	token := seedAPIToken(t, database, "admin")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", false, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/agent-1/persistent", bytes.NewReader([]byte(`{"persistent": true}`)))
	req.SetPathValue("id", "agent-1")
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", "admin@example.com"))
	w := httptest.NewRecorder()
	SetAgentPersistentHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on set persistent, got %d (body=%s)", w.Code, w.Body.String())
	}
	var persistent int
	if err := database.QueryRow(`SELECT persistent FROM agents WHERE agent_id = ?`, "agent-1").Scan(&persistent); err != nil {
		t.Fatalf("read persistent: %v", err)
	}
	if persistent != 1 {
		t.Fatalf("expected persistent=1, got %d", persistent)
	}
	if n := countAuditAction(t, database, "agent.persistent"); n != 1 {
		t.Fatalf("expected 1 persistent audit, got %d", n)
	}
}

func TestAdminApproveReregistrationHandler(t *testing.T) {
	database := setupReregDB(t)
	token := seedAPIToken(t, database, "admin")
	seedPersistentAgent(t, database, "agent-1", "node-1", "NEW", "pending_reregistration", true, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/approve-reregistration", nil)
	req.SetPathValue("id", "agent-1")
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", "admin@example.com"))
	w := httptest.NewRecorder()
	ApproveReregistrationHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on approve re-registration, got %d (body=%s)", w.Code, w.Body.String())
	}
	var state string
	if err := database.QueryRow(`SELECT state FROM agents WHERE agent_id = ?`, "agent-1").Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "pending_apply" {
		t.Fatalf("expected state pending_apply after approval, got %q", state)
	}
	if n := countAuditAction(t, database, "agent.approve_reregistration"); n != 1 {
		t.Fatalf("expected 1 approve audit, got %d", n)
	}
}

func TestAdminSetReregWindowHandler(t *testing.T) {
	database := setupReregDB(t)
	token := seedAPIToken(t, database, "admin")
	seedPersistentAgent(t, database, "agent-1", "node-1", "OLD", "compliant", true, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/reregistration-window", bytes.NewReader([]byte(`{"hours": 4}`)))
	req.SetPathValue("id", "agent-1")
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", "admin@example.com"))
	w := httptest.NewRecorder()
	SetReregistrationWindowHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on set window, got %d (body=%s)", w.Code, w.Body.String())
	}
	var window sql.NullString
	if err := database.QueryRow(`SELECT reregistration_window_until FROM agents WHERE agent_id = ?`, "agent-1").Scan(&window); err != nil {
		t.Fatalf("read window: %v", err)
	}
	if !window.Valid || window.String == "" {
		t.Fatalf("expected a window to be set, got %q", window.String)
	}
	if n := countAuditAction(t, database, "agent.reregistration_window"); n != 1 {
		t.Fatalf("expected 1 window audit, got %d", n)
	}
}
