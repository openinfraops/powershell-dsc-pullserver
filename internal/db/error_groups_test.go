package db

import (
	"database/sql"
	"testing"
	"time"

	"go-dsc-pull/internal/schema"

	_ "modernc.org/sqlite"
)

// setupErrorDB crée une base SQLite en mémoire avec les tables nécessaires au
// regroupement d'erreurs.
func setupErrorDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Force une seule connexion : avec ":memory:", chaque connexion possède sa
	// propre base, donc plusieurs connexions verraient des tables différentes.
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })

	stmts := []string{
		`CREATE TABLE agents (
			agent_id TEXT PRIMARY KEY,
			registered_at TIMESTAMP NULL,
			deleted_at TIMESTAMP NULL
		)`,
		`CREATE TABLE reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id TEXT,
			job_id TEXT
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
		`CREATE INDEX idx_error_occ_fp_lastseen ON error_occurrences(fingerprint, last_seen)`,
		`CREATE INDEX idx_error_occ_node ON error_occurrences(node_id)`,
	}
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return database
}

func seedErrAgent(t *testing.T, database *sql.DB, id string, registeredAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO agents (agent_id, registered_at) VALUES (?, ?)`, id, formatDBTime(registeredAt)); err != nil {
		t.Fatalf("seed agent %s: %v", id, err)
	}
}

func applyErrorGroups(t *testing.T, database *sql.DB, in ErrorGroupWriteInput) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := ApplyErrorGroupsTx(tx, in); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ApplyErrorGroupsTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func TestApplyErrorGroupsTx_GroupsIdenticalErrors(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Deux nœuds, même erreur (GUID et chemin différents) -> une seule empreinte.
	for _, node := range []string{"n1", "n2"} {
		seedErrAgent(t, database, node, base.Add(-time.Hour))
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: node, ReportID: 1,
			Errors: []string{},
			StatusData: []string{
				`{"InDesiredState":false,"ModuleName":"MSFT_x","ResourceId":"R1","Message":"Failed on ` + node + ` accessing C:\\data\\file.txt guid 12345678-1234-1234-1234-123456789012"}`,
			},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}

	groups, err := ListErrorGroups(database, ErrorGroupFilter{})
	if err != nil {
		t.Fatalf("ListErrorGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(groups), groups)
	}
	if groups[0].NodeCount != 2 {
		t.Fatalf("NodeCount = %d, want 2", groups[0].NodeCount)
	}
}

func TestApplyErrorGroupsTx_DistinctHRESULTNeverMerged(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))

	for _, msg := range []string{"Access denied 0x80070005", "Access denied 0x80070006"} {
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: "n1", ReportID: 1,
			Errors: []string{msg}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM error_groups`).Scan(&n); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 groups for distinct HRESULTs, got %d", n)
	}
}

func TestApplyErrorGroupsTx_CategoriesAndRedaction(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))

	applyErrorGroups(t, database, ErrorGroupWriteInput{
		AgentID: "n1", ReportID: 1,
		Errors: []string{"The module MSFT_x is not found password=secret123"}, StatusData: []string{},
		Now: base, Rules: nil, DailyLimit: 1000,
	})

	groups, _ := ListErrorGroups(database, ErrorGroupFilter{})
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Category != "module_missing" {
		t.Fatalf("category = %q, want module_missing", groups[0].Category)
	}
	if groups[0].SampleMessage != "The module MSFT_x is not found password=<REDACTED>" {
		t.Fatalf("sample_message = %q, want redacted", groups[0].SampleMessage)
	}
}

func TestApplyErrorGroupsTx_DailyFingerprintLimit(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))

	// Limite à 2 empreintes/jour pour ce nœud : la 3e erreur distincte ne doit pas
	// créer de groupe supplémentaire.
	limit := 2
	for i := 0; i < 3; i++ {
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: "n1", ReportID: int64(i + 1),
			Errors: []string{"distinct error message number " + string(rune('A'+i))}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: limit,
		})
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM error_groups`).Scan(&n); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if n != limit {
		t.Fatalf("expected %d groups (daily limit), got %d", limit, n)
	}
}

func TestUpdateAndGetErrorGroupStatus(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))
	applyErrorGroups(t, database, ErrorGroupWriteInput{
		AgentID: "n1", ReportID: 1,
		Errors: []string{"boom"}, StatusData: []string{},
		Now: base, Rules: nil, DailyLimit: 1000,
	})
	groups, _ := ListErrorGroups(database, ErrorGroupFilter{})
	fp := groups[0].Fingerprint

	rows, err := UpdateErrorGroupStatus(database, fp, "resolved", "admin@example.com", "fixed by config", base.Add(time.Hour))
	if err != nil || rows != 1 {
		t.Fatalf("UpdateErrorGroupStatus rows=%d err=%v", rows, err)
	}
	g, err := GetErrorGroup(database, fp)
	if err != nil || g == nil {
		t.Fatalf("GetErrorGroup: %v", err)
	}
	if g.Status != "resolved" || g.StatusChangedBy != "admin@example.com" || g.Note != "fixed by config" {
		t.Fatalf("group = %+v, want resolved with note", g)
	}
}

func TestUpdateErrorGroupStatus_Invalid(t *testing.T) {
	database := setupErrorDB(t)
	_, err := UpdateErrorGroupStatus(database, "x", "bogus", "", "", time.Now())
	if err == nil {
		t.Fatalf("expected error for invalid status")
	}
}

func TestRunErrorDetection_Spike(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Spike : 2 nœuds signalent la même erreur dans la dernière heure.
	for _, node := range []string{"n1", "n2"} {
		seedErrAgent(t, database, node, base.Add(-time.Hour))
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: node, ReportID: 1,
			Errors: []string{"Connection timed out"}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}

	res, err := RunErrorDetection(database, ErrorDetectionParams{
		Now: base.Add(30 * time.Minute), SpikeMinNodes: 2, SpikePercent: 10,
		NewWindow: 24 * time.Hour, DeploymentWindow: time.Hour, RepeatDelay: time.Hour, GracePeriod: 0,
	})
	if err != nil {
		t.Fatalf("RunErrorDetection: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 detection result, got %d", len(res))
	}
	if !res[0].Spike || !res[0].Alerted {
		t.Fatalf("expected spike+alerted, got %+v", res[0])
	}
	if res[0].Category != "timeout_network" {
		t.Fatalf("category = %q, want timeout_network", res[0].Category)
	}
}

func TestRunErrorDetection_RepeatDelaySuppresses(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, node := range []string{"n1", "n2"} {
		seedErrAgent(t, database, node, base.Add(-time.Hour))
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: node, ReportID: 1,
			Errors: []string{"Connection timed out"}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}

	// Premier passage : alerté (last_alerted_at mis à jour).
	first, err := RunErrorDetection(database, ErrorDetectionParams{
		Now: base.Add(30 * time.Minute), SpikeMinNodes: 2, SpikePercent: 10,
		NewWindow: 24 * time.Hour, DeploymentWindow: time.Hour, RepeatDelay: time.Hour, GracePeriod: 0,
	})
	if err != nil {
		t.Fatalf("RunErrorDetection: %v", err)
	}
	if !first[0].Alerted {
		t.Fatalf("first run should alert")
	}

	// Second passage immédiat (dans le repeat delay) : supprimé.
	second, err := RunErrorDetection(database, ErrorDetectionParams{
		Now: base.Add(31 * time.Minute), SpikeMinNodes: 2, SpikePercent: 10,
		NewWindow: 24 * time.Hour, DeploymentWindow: time.Hour, RepeatDelay: time.Hour, GracePeriod: 0,
	})
	if err != nil {
		t.Fatalf("RunErrorDetection: %v", err)
	}
	if second[0].Alerted {
		t.Fatalf("second run within repeat delay should be suppressed")
	}
	if !second[0].Suppressed {
		t.Fatalf("second run should be marked suppressed")
	}
}

func TestRunErrorDetection_AfterDeployment(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))
	seedErrAgent(t, database, "n2", base.Add(-time.Hour))

	// Publie une configuration active : name == original_name.
	if _, err := database.Exec(`INSERT INTO configuration_model (name, original_name, upload_date, uploaded_by) VALUES (?, ?, ?, ?)`, "cfg-1", "cfg-1", formatDBTime(base.Add(-time.Hour)), "admin"); err != nil {
		t.Fatalf("insert config: %v", err)
	}

	// Une erreur ingérée après la publication -> first_config_version = cfg-1.
	for _, node := range []string{"n1", "n2"} {
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: node, ReportID: 1,
			Errors: []string{"Checksum mismatch"}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}

	res, err := RunErrorDetection(database, ErrorDetectionParams{
		Now: base, SpikeMinNodes: 5, SpikePercent: 10,
		NewWindow: 24 * time.Hour, DeploymentWindow: 60 * time.Minute, RepeatDelay: 0, GracePeriod: 0,
	})
	if err != nil {
		t.Fatalf("RunErrorDetection: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 detection result, got %d", len(res))
	}
	if !res[0].AfterDeployment {
		t.Fatalf("expected after_deployment, got %+v", res[0])
	}
	if res[0].FirstConfigVersion != "cfg-1" {
		t.Fatalf("first_config_version = %q, want cfg-1", res[0].FirstConfigVersion)
	}
	if res[0].Category != "checksum_download" {
		t.Fatalf("category = %q, want checksum_download", res[0].Category)
	}
}

func TestPurgeErrorOccurrences(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))
	// Une occurrence ancienne (il y a 40 jours) et une récente.
	applyErrorGroups(t, database, ErrorGroupWriteInput{
		AgentID: "n1", ReportID: 1,
		Errors: []string{"old error"}, StatusData: []string{},
		Now: base.Add(-40 * 24 * time.Hour), Rules: nil, DailyLimit: 1000,
	})
	if _, err := database.Exec(`UPDATE error_occurrences SET first_seen = ?, last_seen = ? WHERE node_id = ?`, formatDBTime(base.Add(-40*24*time.Hour)), formatDBTime(base.Add(-40*24*time.Hour)), "n1"); err != nil {
		t.Fatalf("update occurrence age: %v", err)
	}

	removed, err := PurgeErrorOccurrences(database, "sqlite", 30)
	if err != nil {
		t.Fatalf("PurgeErrorOccurrences: %v", err)
	}
	if removed != 1 {
		t.Fatalf("purged = %d, want 1", removed)
	}
}

func TestPurgeEphemeralErrorOccurrences(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedErrAgent(t, database, "n1", base.Add(-time.Hour))
	applyErrorGroups(t, database, ErrorGroupWriteInput{
		AgentID: "n1", ReportID: 1,
		Errors: []string{"ephemeral error"}, StatusData: []string{},
		Now: base, Rules: nil, DailyLimit: 1000,
	})
	// Supprime l'agent : l'occurrence devient orpheline.
	if _, err := database.Exec(`DELETE FROM agents WHERE agent_id = ?`, "n1"); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	removed, err := PurgeEphemeralErrorOccurrences(database)
	if err != nil {
		t.Fatalf("PurgeEphemeralErrorOccurrences: %v", err)
	}
	if removed != 1 {
		t.Fatalf("purged = %d, want 1", removed)
	}
}

func TestListErrorGroupNodes(t *testing.T) {
	database := setupErrorDB(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, node := range []string{"n1", "n2"} {
		seedErrAgent(t, database, node, base.Add(-time.Hour))
		applyErrorGroups(t, database, ErrorGroupWriteInput{
			AgentID: node, ReportID: 1,
			Errors: []string{"shared error"}, StatusData: []string{},
			Now: base, Rules: nil, DailyLimit: 1000,
		})
	}
	groups, _ := ListErrorGroups(database, ErrorGroupFilter{})
	fp := groups[0].Fingerprint
	occ, err := ListErrorGroupNodes(database, fp, 0, 100)
	if err != nil {
		t.Fatalf("ListErrorGroupNodes: %v", err)
	}
	if len(occ) != 2 {
		t.Fatalf("expected 2 occurrences, got %d", len(occ))
	}
}

// TestExtractReportErrorParts_FromSchema exposes the schema helper for the db
// package tests (it is defined in internal/schema, imported here).
func TestErrorFingerprintViaSchema(t *testing.T) {
	a := schema.Fingerprint("MSFT_x", "n1", `Failed on n1 at 192.168.1.1 guid 12345678-1234-1234-1234-123456789012`)
	b := schema.Fingerprint("MSFT_x", "n2", `Failed on n2 at 10.0.0.9 guid abcdef01-2345-6789-abcd-ef0123456789`)
	if a != b {
		t.Fatalf("fingerprints differ for normalized-equivalent messages: %q vs %q", a, b)
	}
}
