package handlers

import (
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/db"

	"golang.org/x/crypto/bcrypt"
)

// createAuthTables crée les tables users et user_api_tokens requises par auth.IsAdmin.
func createAuthTables(t *testing.T, dbConn *sql.DB) {
	t.Helper()
	if _, err := dbConn.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		is_active BOOLEAN DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_logon_date TIMESTAMP,
		role TEXT DEFAULT 'user',
		source TEXT DEFAULT 'local')`); err != nil {
		t.Fatalf("create users table: %v", err)
	}
	if _, err := dbConn.Exec(`CREATE TABLE user_api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token_hash TEXT NOT NULL,
		label TEXT,
		is_active BOOLEAN DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		revoked_at TIMESTAMP)`); err != nil {
		t.Fatalf("create user_api_tokens table: %v", err)
	}
}

// seedAPIToken crée un utilisateur avec le rôle donné et lui associe un token API
// actif ; retourne le token en clair à placer dans l'en-tête Authorization.
func seedAPIToken(t *testing.T, dbConn *sql.DB, role string) string {
	t.Helper()
	createAuthTables(t, dbConn)
	res, err := dbConn.Exec(`INSERT INTO users (first_name, last_name, email, password_hash, role) VALUES (?, ?, ?, ?, ?)`,
		"Test", "User", "test@"+role+".local", "x", role)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userId, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	token, err := db.GenerateAPIToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash token: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO user_api_tokens (user_id, token_hash, label, is_active) VALUES (?, ?, ?, 1)`,
		userId, string(hash), "test"); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return token
}

func seedAgent(t *testing.T, dbConn *sql.DB, agentID string, force int) {
	t.Helper()
	if _, err := dbConn.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state, force_cert_rotation) VALUES (?, ?, ?, ?, ?)`,
		agentID, "NODE", "aabbccddeeff00112233445566778899aabbccdd", "compliant", force); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func forceFlag(t *testing.T, dbConn *sql.DB, agentID string) int {
	t.Helper()
	var force int
	if err := dbConn.QueryRow(`SELECT force_cert_rotation FROM agents WHERE agent_id = ?`, agentID).Scan(&force); err != nil {
		t.Fatalf("read force_cert_rotation: %v", err)
	}
	return force
}

func TestAgentRotateCertificateHandler_Post(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "admin")
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	seedAgent(t, database, agentID, 0)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := forceFlag(t, database, agentID); got != 1 {
		t.Fatalf("expected force_cert_rotation=1 after POST, got %d", got)
	}
}

func TestAgentRotateCertificateHandler_Delete(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "admin")
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	seedAgent(t, database, agentID, 1)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := forceFlag(t, database, agentID); got != 0 {
		t.Fatalf("expected force_cert_rotation=0 after DELETE, got %d", got)
	}
}

func TestAgentRotateCertificateHandler_NonAdminForbidden(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "user")
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	seedAgent(t, database, agentID, 0)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}
	if got := forceFlag(t, database, agentID); got != 0 {
		t.Fatalf("expected force_cert_rotation unchanged (0), got %d", got)
	}
}

func TestAgentRotateCertificateHandler_NoAuthForbidden(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	seedAgent(t, database, agentID, 0)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without auth, got %d", rec.Code)
	}
}

func TestAgentRotateCertificateHandler_NotFound(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "admin")
	const agentID = "missing-agent"

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing agent, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestAgentRotateCertificateHandler_WrongMethod(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "admin")
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	seedAgent(t, database, agentID, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID+"/rotate-certificate", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)

	rec := httptest.NewRecorder()
	AgentRotateCertificateHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestMaybeSetCertificateRotationHeader_ForceFlag vérifie que le drapeau
// force_cert_rotation émet l'en-tête même quand l'activation globale est off et
// que le certificat est hors fenêtre (demande admin « ASAP »).
func TestMaybeSetCertificateRotationHeader_ForceFlag(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, false); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, false)

	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	far := time.Now().UTC().Add(365 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if _, err := database.Exec(`INSERT INTO agents (agent_id, certificate_notafter, force_cert_rotation) VALUES (?, ?, 1)`, agentID, far); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	maybeSetCertificateRotationHeader(rec, database, agentID)
	if got := rec.Header().Get("DSC-certificateRotation"); got != "True" {
		t.Fatalf("expected DSC-certificateRotation: True when force flag set, got %q", got)
	}
}

// TestMaybeSetCertificateRotationHeader_DisabledWithoutForce vérifie que sans
// drapeau force_cert_rotation, un serveur globalement désactivé n'émet pas
// l'en-tête même si le certificat est dans la fenêtre de renouvellement.
func TestMaybeSetCertificateRotationHeader_DisabledWithoutForce(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, false); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, false)

	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	near := time.Now().UTC().Add(5 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if _, err := database.Exec(`INSERT INTO agents (agent_id, certificate_notafter, force_cert_rotation) VALUES (?, ?, 0)`, agentID, near); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	maybeSetCertificateRotationHeader(rec, database, agentID)
	if got := rec.Header().Get("DSC-certificateRotation"); got != "" {
		t.Fatalf("expected no DSC-certificateRotation header when disabled & not forced, got %q", got)
	}
}

// TestCertificateRotationHandler_ForceFlagAcceptsWhenDisabled vérifie qu'une
// rotation explicitement demandée par un admin (force_cert_rotation=1) est
// acceptée même si l'activation globale en base est off.
func TestCertificateRotationHandler_ForceFlagAcceptsWhenDisabled(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, false); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	oldThumb := strings.ToLower(hex.EncodeToString(sum[:]))
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state, force_cert_rotation) VALUES (?, ?, ?, ?, 1)`,
		agentID, "NODE", oldThumb, "compliant"); err != nil {
		t.Fatal(err)
	}

	const newThumb = "aabbccddeeff00112233445566778899aabbccdd"
	body := fmt.Sprintf(`{"RotationInformation":{"CertificateInformation":{"Thumbprint":"%s","Subject":"NewSubj","Issuer":"NewIssuer","NotBefore":"2026-01-01T00:00:00Z","NotAfter":"2031-01-01T00:00:00Z"}}}`, newThumb)
	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/CertificateRotation", strings.NewReader(body))
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	CertificateRotationHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 when force-flagged even if disabled, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := forceFlag(t, database, agentID); got != 0 {
		t.Fatalf("expected force_cert_rotation cleared after rotation, got %d", got)
	}
}
