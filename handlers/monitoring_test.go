package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

// setupMonitoringDB crée une base SQLite en mémoire avec les tables utilisées
// par les endpoints /health et /metrics (agents, scheduler_tasks,
// dsc_infra_info) et y insère une ligne d'infrastructure.
func setupMonitoringDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		node_name TEXT,
		state TEXT,
		last_communication TIMESTAMP,
		deleted_at TIMESTAMP NULL,
		deleted_by TEXT NULL,
		reboot_pending INTEGER DEFAULT 0,
		reboot_pending_since TIMESTAMP NULL,
		reboot_requested_by TEXT NULL,
		reboot_loop INTEGER DEFAULT 0,
		reboot_request_count INTEGER DEFAULT 0
	)`); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE scheduler_tasks (
		task_name TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		next_run_at TIMESTAMP NULL,
		last_run_at TIMESTAMP NULL,
		last_status TEXT NOT NULL DEFAULT 'idle',
		last_message TEXT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create scheduler_tasks: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE dsc_infra_info (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		latest_release TEXT,
		latest_release_url TEXT,
		update_available INTEGER DEFAULT 0,
		release_check_ok INTEGER DEFAULT 0,
		release_checked_at TIMESTAMP
	)`); err != nil {
		t.Fatalf("create dsc_infra_info: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO dsc_infra_info (id, latest_release, update_available, release_check_ok) VALUES (1, '1.8.0', 1, 1)`); err != nil {
		t.Fatalf("seed dsc_infra_info: %v", err)
	}
	return database
}

func setMetricsToken(t *testing.T, token string) {
	t.Helper()
	global.AppConfig = &schema.AppConfig{WebUI: schema.HTTPConfig{MetricsToken: token}}
	t.Cleanup(func() { global.AppConfig = nil })
}

func TestHealthHandler(t *testing.T) {
	database := setupMonitoringDB(t)
	h := HealthHandler(database)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"ok"`, `"version":"dev"`, `"reachable":true`, `"alive":true`, `"running_tasks":0`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, missing %q", body, want)
		}
	}
}

func TestHealthHandler_DBUnreachable(t *testing.T) {
	database := setupMonitoringDB(t)
	// Close the DB so the query fails -> degraded / not alive.
	database.Close()
	h := HealthHandler(database)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"degraded"`, `"reachable":false`, `"alive":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, missing %q", body, want)
		}
	}
}

func TestMetricsHandler_NotExposedWhenNoToken(t *testing.T) {
	database := setupMonitoringDB(t)
	setMetricsToken(t, "") // empty token -> /metrics disabled

	h := MetricsHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (not exposed)", rec.Code)
	}
}

func TestMetricsHandler_TokenRequired(t *testing.T) {
	database := setupMonitoringDB(t)
	setMetricsToken(t, "super-secret")

	h := MetricsHandler(database)

	// Missing token -> 401.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	// Wrong token -> 401.
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", rec.Code)
	}

	// Correct token via query param -> 200.
	req = httptest.NewRequest(http.MethodGet, "/metrics?token=super-secret", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want 200", rec.Code)
	}
}

func TestMetricsHandler_Renders(t *testing.T) {
	database := setupMonitoringDB(t)
	// Deux nœuds actifs (un OK, un NULL->unknown) et un nœud supprimé (ignoré).
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, state, last_communication) VALUES ('a1', 'node-a', 'OK', '2026-01-01 00:00:00')`)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, state, last_communication) VALUES ('a3', 'node-c', NULL, '2026-01-02 00:00:00')`)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, state, last_communication, deleted_at) VALUES ('a2', 'node-b', 'Success', '2026-01-02 00:00:00', '2026-01-03 00:00:00')`)
	mustExec(t, database, `INSERT INTO scheduler_tasks (task_name, display_name, last_status, next_run_at, last_run_at) VALUES ('report_cleanup', 'Report cleanup', 'idle', '2026-01-01 00:00:05', '2026-01-01 00:00:00')`)
	mustExec(t, database, `INSERT INTO scheduler_tasks (task_name, display_name, last_status) VALUES ('release_check', 'Release check', 'success')`)

	setMetricsToken(t, "super-secret")

	h := MetricsHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`dsc_build_info{version="dev"} 1`,
		`dsc_db_up 1`,
		`dsc_nodes 2`, // only non-deleted nodes
		`dsc_nodes_by_state{state="OK"} 1`,
		`dsc_nodes_by_state{state="unknown"} 1`,
		`dsc_last_report_age_seconds `, // value present, not asserted exactly
		`dsc_scheduler_task_last_status{task="report_cleanup",status="idle"} 1`,
		`dsc_scheduler_task_last_status{task="release_check",status="success"} 1`,
		`dsc_scheduler_task_last_run_timestamp_seconds{task="report_cleanup"} `,
		`dsc_scheduler_task_next_run_timestamp_seconds{task="report_cleanup"} `,
		`dsc_http_responses_total{code="401"} `,
		`dsc_http_responses_total{code="403"} `,
		`dsc_update_available 1`,
		`dsc_release_check_ok 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q\n---\n%s", want, body)
		}
	}
}

func TestMetricsHandler_DBUnreachable(t *testing.T) {
	database := setupMonitoringDB(t)
	setMetricsToken(t, "super-secret")
	database.Close() // simulate a DB outage

	h := MetricsHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// The DB-up gauge is reported exactly once (value 0); build info is still
	// emitted and DB-derived gauges are skipped.
	if !strings.Contains(body, `dsc_db_up 0`) {
		t.Fatalf("body missing dsc_db_up 0\n%s", body)
	}
	if strings.Count(body, "dsc_db_up 0") != 1 {
		t.Fatalf("dsc_db_up 0 should be emitted exactly once\n%s", body)
	}
	if strings.Contains(body, `dsc_db_up 1`) {
		t.Fatalf("body should not contain dsc_db_up 1\n%s", body)
	}
	if !strings.Contains(body, `dsc_build_info{version="dev"} 1`) {
		t.Fatalf("body missing dsc_build_info\n%s", body)
	}
	// DB-derived gauges are skipped when the DB is down.
	if strings.Contains(body, "dsc_nodes ") {
		t.Fatalf("body should not contain dsc_nodes when DB is down\n%s", body)
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
