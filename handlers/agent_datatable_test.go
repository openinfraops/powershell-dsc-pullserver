package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

// TestListAgentsDataTable_CertAtRisk verifie que le filtre serverSide
// cert_at_risk=true reduit bien la liste aux noeuds dont le certificat est
// expire ou expire bientot (plus les noeuds sans certificat), et EXCLUT les
// noeuds dont le certificat est valide et loin de l'expiration.
func TestListAgentsDataTable_CertAtRisk(t *testing.T) {
	database := setupAgentsDB(t)

	// Trois noeuds : certificat expire, certificat valide (lointain), aucun cert.
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_notafter, state) VALUES (?, ?, ?, ?)`,
		"at-risk", "AT-RISK", "2020-01-01 00:00:00", "success"); err != nil {
		t.Fatalf("seed at-risk: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_notafter, state) VALUES (?, ?, ?, ?)`,
		"valid", "VALID", "2030-01-01 00:00:00", "success"); err != nil {
		t.Fatalf("seed valid: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_notafter, state) VALUES (?, ?, ?, ?)`,
		"no-cert", "NO-CERT", "", "success"); err != nil {
		t.Fatalf("seed no-cert: %v", err)
	}

	// Injecte la base partagee et la config (driver sqlite).
	oldShared := db.Shared()
	oldAppConfig := global.AppConfig
	defer func() {
		db.SetShared(oldShared)
		global.AppConfig = oldAppConfig
	}()
	db.SetShared(database)
	global.AppConfig = &schema.AppConfig{
		Database: schema.DBConfig{Driver: "sqlite"},
	}

	q := url.Values{}
	q.Set("draw", "1")
	q.Set("start", "0")
	q.Set("length", "25")
	q.Set("order[0][column]", "1")
	q.Set("order[0][dir]", "asc")
	q.Set("cert_at_risk", "true")
	q.Set("search[value]", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	ListAgentsDataTableHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Draw            int `json:"draw"`
		RecordsTotal    int `json:"recordsTotal"`
		RecordsFiltered int `json:"recordsFiltered"`
		Data            []struct {
			AgentID string `json:"agent_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}

	// Avec le filtre, le noeud a certificat valide (2030) doit etre EXCLU.
	for _, row := range resp.Data {
		if row.AgentID == "valid" {
			t.Fatalf("expected 'valid' node to be filtered out, but it was returned; data=%+v", resp.Data)
		}
	}
	// Au moins le noeud a risque (et le noeud sans cert) doivent etre presents.
	seen := map[string]bool{}
	for _, row := range resp.Data {
		seen[row.AgentID] = true
	}
	if !seen["at-risk"] {
		t.Fatalf("expected at-risk node to be returned, but it was not; data=%+v", resp.Data)
	}
	if !seen["no-cert"] {
		t.Fatalf("expected no-cert node to be returned (clause includes empty certs); data=%+v", resp.Data)
	}
}
