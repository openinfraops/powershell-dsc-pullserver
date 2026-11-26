package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// setupErrorHandlerDB crée une base avec les tables nécessaires aux endpoints
// error_groups (agents + error_groups + error_occurrences + configuration_model
// + agent_tags + audit).
func setupErrorHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	database := setupAgentsDB(t)
	// Force une seule connexion : avec ":memory:", chaque connexion a sa propre
	// base, et les handlers enchaînent plusieurs requêtes.
	database.SetMaxOpenConns(1)
	stmts := []string{
		`CREATE TABLE error_groups (
			fingerprint TEXT PRIMARY KEY,
			category TEXT,
			resource_type TEXT,
			sample_message TEXT,
			first_seen TIMESTAMP,
			last_seen TIMESTAMP,
			first_config_version TEXT,
			status TEXT DEFAULT 'open',
			status_changed_by TEXT,
			status_changed_at TIMESTAMP,
			note TEXT,
			last_alerted_at TIMESTAMP
		)`,
		`CREATE TABLE error_occurrences (
			fingerprint TEXT,
			node_id TEXT,
			first_seen TIMESTAMP,
			last_seen TIMESTAMP,
			count INTEGER DEFAULT 1,
			last_report_id INTEGER,
			PRIMARY KEY (fingerprint, node_id)
		)`,
		`CREATE TABLE configuration_model (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			original_name TEXT,
			upload_date TIMESTAMP,
			uploaded_by TEXT
		)`,
		`CREATE TABLE agent_tags (
			agent_id TEXT,
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

func seedErrorGroup(t *testing.T, database *sql.DB, fp, category, resourceType, sample, firstSeen, lastSeen, status, note string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO error_groups (fingerprint, category, resource_type, sample_message, first_seen, last_seen, status, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, fp, category, resourceType, sample, firstSeen, lastSeen, status, note); err != nil {
		t.Fatalf("seed error group: %v", err)
	}
}

func seedErrorOccurrence(t *testing.T, database *sql.DB, fp, node string, count int) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO error_occurrences (fingerprint, node_id, first_seen, last_seen, count, last_report_id)
		VALUES (?, ?, '2026-01-01 12:00:00', '2026-01-01 12:00:00', ?, 1)`, fp, node, count); err != nil {
		t.Fatalf("seed error occurrence: %v", err)
	}
}

func TestErrorGroupsListHandler(t *testing.T) {
	database := setupErrorHandlerDB(t)
	injectShared(t, database, false)

	seedErrorGroup(t, database, "aa", "access_denied", "MSFT_x", "Access denied", "2026-01-01 12:00:00", "2026-01-01 12:00:00", "open", "")
	seedErrorOccurrence(t, database, "aa", "n1", 3)
	seedErrorOccurrence(t, database, "aa", "n2", 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/error_groups", nil)
	rec := httptest.NewRecorder()
	ErrorGroupsListHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Groups []struct {
			Fingerprint string `json:"fingerprint"`
			Category    string `json:"category"`
			NodeCount   int    `json:"node_count"`
		} `json:"groups"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Groups) != 1 || out.Total != 1 {
		t.Fatalf("expected 1 group, got %d (total %d)", len(out.Groups), out.Total)
	}
	if out.Groups[0].Category != "access_denied" || out.Groups[0].NodeCount != 2 {
		t.Fatalf("group = %+v", out.Groups[0])
	}
}

func TestErrorGroupGetHandler_EscapesSampleMessage(t *testing.T) {
	database := setupErrorHandlerDB(t)
	injectShared(t, database, false)

	seedErrorGroup(t, database, "bb", "unknown", "", `<script>alert(1)</script>`, "2026-01-01 12:00:00", "2026-01-01 12:00:00", "open", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/error_groups/bb", nil)
	req.SetPathValue("fingerprint", "bb")
	rec := httptest.NewRecorder()
	ErrorGroupGetHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		SampleMessage string `json:"sample_message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if strings.Contains(out.SampleMessage, "<script>") {
		t.Fatalf("sample_message not escaped: %s", out.SampleMessage)
	}
	if out.SampleMessage != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Fatalf("escaped sample_message = %q, want the HTML-escaped form", out.SampleMessage)
	}
}

func TestErrorGroupNodesHandler(t *testing.T) {
	database := setupErrorHandlerDB(t)
	injectShared(t, database, false)

	seedErrorGroup(t, database, "cc", "timeout_network", "MSFT_y", "timed out", "2026-01-01 12:00:00", "2026-01-01 12:00:00", "open", "")
	seedErrorOccurrence(t, database, "cc", "n1", 2)
	seedErrorOccurrence(t, database, "cc", "n2", 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/error_groups/cc/nodes", nil)
	req.SetPathValue("fingerprint", "cc")
	rec := httptest.NewRecorder()
	ErrorGroupNodesHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Nodes []struct {
			NodeID string `json:"node_id"`
			Count  int    `json:"count"`
		} `json:"nodes"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(out.Nodes))
	}
}

func TestErrorGroupUpdateHandler_Admin(t *testing.T) {
	database := setupErrorHandlerDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "admin")

	seedErrorGroup(t, database, "dd", "access_denied", "", "denied", "2026-01-01 12:00:00", "2026-01-01 12:00:00", "open", "")

	body := `{"status":"resolved","note":"fixed"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/error_groups/dd", strings.NewReader(body))
	req.SetPathValue("fingerprint", "dd")
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	ErrorGroupUpdateHandler(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}

	var status, note string
	if err := database.QueryRow(`SELECT status, note FROM error_groups WHERE fingerprint = ?`, "dd").Scan(&status, &note); err != nil {
		t.Fatalf("read group: %v", err)
	}
	if status != "resolved" || note != "fixed" {
		t.Fatalf("group status=%q note=%q, want resolved/fixed", status, note)
	}

	// Un audit a été écrit.
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE action = 'error_group.resolved'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 audit entry, got %d", n)
	}
}

func TestErrorGroupUpdateHandler_NonAdminForbidden(t *testing.T) {
	database := setupErrorHandlerDB(t)
	injectShared(t, database, false)
	token := seedAPIToken(t, database, "user") // non-admin

	seedErrorGroup(t, database, "ee", "access_denied", "", "denied", "2026-01-01 12:00:00", "2026-01-01 12:00:00", "open", "")

	req := httptest.NewRequest(http.MethodPut, "/api/v1/error_groups/ee", strings.NewReader(`{"status":"resolved"}`))
	req.SetPathValue("fingerprint", "ee")
	req.Header.Set("Authorization", "Token "+token)
	rec := httptest.NewRecorder()
	ErrorGroupUpdateHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}
