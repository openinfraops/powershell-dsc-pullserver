package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"go-dsc-pull/internal/global"
)

// setupTagTestDB crée une base SQLite en mémoire avec les tables nécessaires
// aux handlers de tags et à leur audit (agents, agent_tags, audit).
func setupTagTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database := setupAgentsDB(t)
	// Avec ":memory:", chaque connexion du pool est une base VIDE distincte.
	// Le handler de pré-enrôlement lance une compilation en arrière-plan qui
	// ouvre une seconde connexion (base vide, sans schéma), ce qui ferait
	// échouer les requêtes suivantes du test ("no such table"). On force donc
	// une connexion unique pour que toutes partagent le même schéma.
	database.SetMaxOpenConns(1)
	stmts := []string{
		`CREATE TABLE agent_tags (
			agent_id TEXT NOT NULL,
			tag_key TEXT NOT NULL,
			tag_value TEXT NOT NULL,
			PRIMARY KEY (agent_id, tag_key, tag_value)
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
	}
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return database
}

// tagTestAppConfig injecte la base partagée et définit une config applicative
// minimale avec driver sqlite (utilisée par les handlers de tags).
func tagTestAppConfig(t *testing.T, database *sql.DB) {
	t.Helper()
	injectShared(t, database, false)
	global.AppConfig.Database.Driver = "sqlite"
}

func seedAgentTag(t *testing.T, database *sql.DB, agentID, key, value string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO agent_tags (agent_id, tag_key, tag_value) VALUES (?, ?, ?)`, agentID, key, value); err != nil {
		t.Fatalf("seed agent tag: %v", err)
	}
}

func countAgentTags(t *testing.T, database *sql.DB, agentID, key, value string) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agent_tags WHERE agent_id = ? AND tag_key = ? AND tag_value = ?`, agentID, key, value).Scan(&n); err != nil {
		t.Fatalf("count agent tags: %v", err)
	}
	return n
}

// --- Un utilisateur en lecture seule (role "user") doit être refusé (403) ---

func TestAgentTagsSetHandler_NonAdminForbidden(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "user")
	const agentID = "agent-1"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, agentID, "NODE"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/"+agentID+"/tags", strings.NewReader(`{"key":"environment","value":"prod"}`))
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	AgentTagsSetHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestAgentTagsDeleteHandler_NonAdminForbidden(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "user")
	const agentID = "agent-1"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, agentID, "NODE"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/tags?key=environment&value=prod", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	AgentTagsDeleteHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestBulkAgentsTagsHandler_NonAdminForbidden(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "user")
	body := strings.NewReader(`{"agent_ids":["agent-1"],"key":"environment","value":"prod"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/bulk/tags", body)
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	BulkAgentsTagsHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestPreEnrollAgentHandler_NonAdminForbidden(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "user")
	body := strings.NewReader(`{"node_name":"node-prod","tags":[{"key":"environment","value":"prod"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/preenroll", body)
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	PreEnrollAgentHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// --- Un admin peut modifier les tags et l'action est auditée (avant/après + acteur) ---

func TestAgentTagsSetHandler_AdminAudit(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "admin")
	const actorEmail = "test@admin.local"
	const agentID = "agent-1"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, agentID, "NODE"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	seedAgentTag(t, database, agentID, "environment", "dev")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/"+agentID+"/tags", strings.NewReader(`{"key":"environment","value":"prod"}`))
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", actorEmail))
	rec := httptest.NewRecorder()
	AgentTagsSetHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if countAgentTags(t, database, agentID, "environment", "prod") != 1 {
		t.Fatalf("expected environment=prod to be set")
	}

	// Audit : action, cible, acteur, et ancienne valeur avant le changement.
	var userEmail, action, target, details string
	if err := database.QueryRow(`SELECT user_email, action, target, details FROM audit ORDER BY id DESC LIMIT 1`).Scan(&userEmail, &action, &target, &details); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if action != "tag.set" {
		t.Fatalf("expected action 'tag.set', got %q", action)
	}
	if target != "agent" {
		t.Fatalf("expected target 'agent', got %q", target)
	}
	if userEmail != actorEmail {
		t.Fatalf("expected actor %q, got %q", actorEmail, userEmail)
	}
	if !strings.Contains(details, "environment=prod") || !strings.Contains(details, "previous: dev") {
		t.Fatalf("expected audit to record old/new environment value, got details %q", details)
	}
}

func TestAgentTagsDeleteHandler_AdminAudit(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "admin")
	const actorEmail = "test@admin.local"
	const agentID = "agent-1"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, agentID, "NODE"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	seedAgentTag(t, database, agentID, "environment", "prod")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/tags?key=environment&value=prod", nil)
	req.SetPathValue("id", agentID)
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", actorEmail))
	rec := httptest.NewRecorder()
	AgentTagsDeleteHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if countAgentTags(t, database, agentID, "environment", "prod") != 0 {
		t.Fatalf("expected environment=prod to be deleted")
	}

	var userEmail, action, details string
	if err := database.QueryRow(`SELECT user_email, action, details FROM audit ORDER BY id DESC LIMIT 1`).Scan(&userEmail, &action, &details); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if action != "tag.delete" {
		t.Fatalf("expected action 'tag.delete', got %q", action)
	}
	if userEmail != actorEmail {
		t.Fatalf("expected actor %q, got %q", actorEmail, userEmail)
	}
	if !strings.Contains(details, "environment=prod") || !strings.Contains(details, "previous: prod") {
		t.Fatalf("expected audit to record deleted environment value, got details %q", details)
	}
}

func TestBulkAgentsTagsHandler_AdminAudit(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "admin")
	const actorEmail = "test@admin.local"
	for _, id := range []string{"agent-1", "agent-2"} {
		if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, id, "NODE"); err != nil {
			t.Fatalf("seed agent %s: %v", id, err)
		}
	}
	body := strings.NewReader(`{"agent_ids":["agent-1","agent-2"],"key":"environment","value":"prod"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/bulk/tags", body)
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", actorEmail))
	rec := httptest.NewRecorder()
	BulkAgentsTagsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if countAgentTags(t, database, "agent-1", "environment", "prod") != 1 || countAgentTags(t, database, "agent-2", "environment", "prod") != 1 {
		t.Fatalf("expected bulk tag to be applied to both agents")
	}
	var userEmail, action, details string
	if err := database.QueryRow(`SELECT user_email, action, details FROM audit ORDER BY id DESC LIMIT 1`).Scan(&userEmail, &action, &details); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if action != "tag.set" {
		t.Fatalf("expected action 'tag.set', got %q", action)
	}
	if userEmail != actorEmail {
		t.Fatalf("expected actor %q, got %q", actorEmail, userEmail)
	}
	if !strings.Contains(details, "environment=prod") {
		t.Fatalf("expected audit to mention the bulk tag, got %q", details)
	}
}

func TestPreEnrollAgentHandler_AdminAppliesTags(t *testing.T) {
	database := setupTagTestDB(t)
	tagTestAppConfig(t, database)
	token := seedAPIToken(t, database, "admin")
	const actorEmail = "test@admin.local"
	body := strings.NewReader(`{"node_name":"node-prod","tags":[{"key":"environment","value":"prod"},{"key":"team","value":"a"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/preenroll", body)
	req.Header.Set("Authorization", "Token "+token)
	req = req.WithContext(context.WithValue(req.Context(), "userId", actorEmail))
	rec := httptest.NewRecorder()
	PreEnrollAgentHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	// The temp agent id is a SHA1-derived value, not known here; just verify the
	// environment=prod tag was actually applied by the pre-enrollment and that
	// the audit recorded it.
	var envCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agent_tags WHERE tag_key='environment' AND tag_value='prod'`).Scan(&envCount); err != nil {
		t.Fatalf("count env tag: %v", err)
	}
	if envCount == 0 {
		t.Fatalf("expected environment=prod tag applied at preenroll")
	}
	var userEmail, action, details string
	if err := database.QueryRow(`SELECT user_email, action, details FROM audit WHERE action='preenroll' ORDER BY id DESC LIMIT 1`).Scan(&userEmail, &action, &details); err != nil {
		t.Fatalf("query preenroll audit: %v", err)
	}
	if userEmail != actorEmail {
		t.Fatalf("expected actor %q, got %q", actorEmail, userEmail)
	}
	if !strings.Contains(details, "environment=prod") {
		t.Fatalf("expected audit to mention the applied environment tag, got %q", details)
	}
}

// --- L'enregistrement d'un nœud ne doit pas permettre de poser des tags ---

func dscRegistrationSignature(body []byte, xmsDate, key string) string {
	h := sha256.Sum256(body)
	bodyHash := base64.StdEncoding.EncodeToString(h[:])
	stringToSign := bodyHash + "\n" + xmsDate
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestRegisterHandler_CannotSetEnvironmentTag(t *testing.T) {
	database := setupAgentsDB(t)
	for _, s := range []string{
		`CREATE TABLE agent_configurations (agent_id TEXT, configuration_name TEXT)`,
		`CREATE TABLE agent_tags (
			agent_id TEXT NOT NULL,
			tag_key TEXT NOT NULL,
			tag_value TEXT NOT NULL,
			PRIMARY KEY (agent_id, tag_key, tag_value)
		)`,
	} {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	tagTestAppConfig(t, database)
	const regKey = "test-registration-key"
	global.AppConfig.DSCPullServer.RegistrationKey = regKey

	const agentID = "11111111-2222-3333-4444-555555555555"
	// Le corps contient un champ Tags (environment=prod) qu'un nœud pourrait
	// tenter d'envoyer pour s'auto-attribuer l'environnement.
	body := []byte(`{
		"AgentInformation":{"NodeName":"node-prod","LCMVersion":"2.0"},
		"RegistrationInformation":{"RegistrationMessageType":"Initial","CertificateInformation":{"Thumbprint":"aabbccddeeff00112233445566778899aabbccdd","Subject":"subj","Issuer":"iss","NotBefore":"2026-01-01T00:00:00Z","NotAfter":"2031-01-01T00:00:00Z"}},
		"Tags":[{"key":"environment","value":"prod"}]
	}`)
	const xmsDate = "2026-11-27T00:00:00Z"
	sig := dscRegistrationSignature(body, xmsDate, regKey)

	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')", strings.NewReader(string(body)))
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.Header.Set("x-ms-date", xmsDate)
	req.Header.Set("Authorization", "Shared "+sig)

	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	// Le nœud est bien enregistré…
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ?`, agentID).Scan(&n); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected agent %s to be registered", agentID)
	}
	// …mais aucun tag environment=prod ne doit avoir été posé par le nœud.
	if err := database.QueryRow(`SELECT COUNT(*) FROM agent_tags WHERE tag_key='environment' AND tag_value='prod'`).Scan(&n); err != nil {
		t.Fatalf("count agent tags: %v", err)
	}
	if n != 0 {
		t.Fatalf("node assigned itself environment=prod at registration (%d rows)", n)
	}
}
