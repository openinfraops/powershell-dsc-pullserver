package db

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupRebootDB crée une base SQLite en mémoire avec la table agents et les
// colonnes d'état dérivé du redémarrage.
func setupRebootDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		reboot_pending INTEGER DEFAULT 0,
		reboot_pending_since TIMESTAMP NULL,
		reboot_requested_by TEXT NULL,
		reboot_loop INTEGER DEFAULT 0,
		reboot_request_count INTEGER DEFAULT 0
	)`); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	return database
}

// applyReboot exécute ApplyRebootReportState dans une transaction et la commit.
func applyReboot(t *testing.T, database *sql.DB, agentId string, requested bool, requester string, now time.Time, thr, win int) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := ApplyRebootReportState(tx, agentId, requested, requester, now, thr, win); err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply reboot state: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

// seedAgent insère une ligne agent (état dérivé vide).
func seedAgent(t *testing.T, database *sql.DB, agentId string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO agents (agent_id) VALUES (?)`, agentId); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

// rebootState lit l'état dérivé d'un agent. `since` est renvoyé sous forme de
// time.Time (via parseDBTime) pour absorber la conversion du pilote SQLite qui
// transforme une colonne TIMESTAMP en time.Time.
func rebootState(t *testing.T, database *sql.DB, agentId string) (pending bool, since time.Time, requester string, loop bool, count int) {
	t.Helper()
	var pendingI, loopI, countI int
	var sinceRaw interface{}
	var requesterS sql.NullString
	if err := database.QueryRow(`SELECT reboot_pending, reboot_pending_since, reboot_requested_by, reboot_loop, reboot_request_count FROM agents WHERE agent_id = ?`, agentId).
		Scan(&pendingI, &sinceRaw, &requesterS, &loopI, &countI); err != nil {
		t.Fatalf("read reboot state: %v", err)
	}
	since, _ = parseDBTime(sinceRaw)
	return pendingI == 1, since, requesterS.String, loopI == 1, countI
}

func TestApplyRebootReportState_FirstRequest(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	applyReboot(t, database, "a1", true, "Mod:Res", base, 3, 60)

	pending, since, requester, loop, count := rebootState(t, database, "a1")
	if !pending {
		t.Fatalf("pending = false, want true")
	}
	if !since.Equal(base) {
		t.Fatalf("since = %v, want %v", since, base)
	}
	if requester != "Mod:Res" {
		t.Fatalf("requester = %q, want Mod:Res", requester)
	}
	if loop {
		t.Fatalf("loop = true on first request, want false")
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestApplyRebootReportState_ConsecutiveRequests(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	applyReboot(t, database, "a1", true, "Mod:Old", base, 3, 60)
	applyReboot(t, database, "a1", true, "Mod:New", base.Add(time.Minute), 3, 60)

	pending, since, requester, loop, count := rebootState(t, database, "a1")
	if !pending {
		t.Fatalf("pending = false, want true")
	}
	// pending_since reste figé sur la première requête de la série.
	if !since.Equal(base) {
		t.Fatalf("since = %v, want %v (unchanged)", since, base)
	}
	if requester != "Mod:New" {
		t.Fatalf("requester = %q, want Mod:New (latest)", requester)
	}
	if loop {
		t.Fatalf("loop = true, want false (count < threshold)")
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestApplyRebootReportState_ClearOnFalse(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	applyReboot(t, database, "a1", true, "Mod:Res", base, 3, 60)
	applyReboot(t, database, "a1", false, "", base.Add(time.Minute), 3, 60)

	pending, since, requester, loop, count := rebootState(t, database, "a1")
	if pending {
		t.Fatalf("pending = true, want false")
	}
	if !since.IsZero() {
		t.Fatalf("since = %v, want zero (cleared)", since)
	}
	if requester != "" {
		t.Fatalf("requester = %q, want empty (cleared)", requester)
	}
	if loop {
		t.Fatalf("loop = true, want false")
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

func TestApplyRebootReportState_LoopDetection(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Trois rapports consécutifs espacés de plus de 60 min -> boucle détectée.
	applyReboot(t, database, "a1", true, "Mod:Res", base, 3, 60)
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(30*time.Minute), 3, 60)
	// À 61 min, count = 3 et la série dure >= 60 min.
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(61*time.Minute), 3, 60)

	_, _, _, loop, count := rebootState(t, database, "a1")
	if !loop {
		t.Fatalf("loop = false, want true (3 requests over >= 60 min)")
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	// Un rapport sans demande efface la boucle.
	applyReboot(t, database, "a1", false, "", base.Add(62*time.Minute), 3, 60)
	_, _, _, loop, count = rebootState(t, database, "a1")
	if loop {
		t.Fatalf("loop = true after clear, want false")
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 after clear", count)
	}
}

func TestApplyRebootReportState_LoopNotTriggeredByCountAlone(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Trois rapports rapprochés (< 60 min) : count atteint le seuil mais la
	// fenêtre minimale n'est pas satisfaite -> pas de boucle.
	applyReboot(t, database, "a1", true, "Mod:Res", base, 3, 60)
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(time.Minute), 3, 60)
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(2*time.Minute), 3, 60)

	_, _, _, loop, count := rebootState(t, database, "a1")
	if loop {
		t.Fatalf("loop = true, want false (window < 60 min)")
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestApplyRebootReportState_DefaultThresholds(t *testing.T) {
	database := setupRebootDB(t)
	seedAgent(t, database, "a1")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// thr=0 et win=0 doivent utiliser les défauts (3 / 60 min).
	applyReboot(t, database, "a1", true, "Mod:Res", base, 0, 0)
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(61*time.Minute), 0, 0)
	applyReboot(t, database, "a1", true, "Mod:Res", base.Add(62*time.Minute), 0, 0)

	_, _, _, loop, count := rebootState(t, database, "a1")
	if !loop {
		t.Fatalf("loop = false with defaults, want true")
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

func TestApplyRebootReportState_AbsentAgentNoop(t *testing.T) {
	database := setupRebootDB(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Aucune ligne : l'appel ne doit pas créer d'état fantôme.
	applyReboot(t, database, "ghost", true, "Mod:Res", base, 3, 60)

	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ?`, "ghost").Scan(&n); err != nil {
		t.Fatalf("count ghost: %v", err)
	}
	if n != 0 {
		t.Fatalf("ghost agent created (%d rows), want 0", n)
	}
}
