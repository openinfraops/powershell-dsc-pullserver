package db

import (
	"database/sql"
	"time"
)

// Default reboot-loop detection thresholds, used when the corresponding
// configuration keys (dsc_pullserver.reboot_loop_threshold /
// reboot_loop_window_mins) are left at their zero value.
const (
	DefaultRebootLoopThreshold  = 3
	DefaultRebootLoopWindowMins = 60
)

// ApplyRebootReportState updates the derived reboot state of an agent, in the
// same transaction as the report insert/update, so the report and the node
// record stay consistent.
//
// Semantics (from issue #94):
//   - rebootRequested=true: marks the node pending, records the latest
//     requester, and increments the consecutive request counter. On the first
//     report of a new streak (node was not pending) the counter is reset to 1
//     and reboot_pending_since is frozen at `now`.
//   - rebootRequested=false: clears the pending state, resets the counter, the
//     requester and the loop flag.
//   - reboot_loop is recomputed on every report: it is true when the counter
//     reaches `loopThreshold` (N) consecutive reports AND the streak has lasted
//     at least `loopWindowMins` (M) minutes.
//
// When the agent row does not exist the function is a no-op: the caller is
// expected to have ensured the node registration (upsert) before ingesting a
// report, and an orphan report should not create a phantom reboot state.
func ApplyRebootReportState(tx *sql.Tx, agentId string, rebootRequested bool, requester string, now time.Time, loopThreshold int, loopWindowMins int) error {
	if loopThreshold <= 0 {
		loopThreshold = DefaultRebootLoopThreshold
	}
	if loopWindowMins <= 0 {
		loopWindowMins = DefaultRebootLoopWindowMins
	}

	var pending bool
	var pendingSince interface{}
	var count int
	err := tx.QueryRow(`SELECT reboot_pending, reboot_pending_since, reboot_request_count FROM agents WHERE agent_id = ?`, agentId).
		Scan(&pending, &pendingSince, &count)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	// Report sans demande de redémarrage : on efface l'état dérivé.
	if !rebootRequested {
		_, err = tx.Exec(`UPDATE agents SET reboot_pending = 0, reboot_pending_since = NULL, reboot_requested_by = NULL, reboot_loop = 0, reboot_request_count = 0 WHERE agent_id = ?`, agentId)
		return err
	}

	// Report avec demande de redémarrage.
	if !pending || count < 1 {
		// Nouvelle série : on repart de 1 et on fige l'horodatage de départ.
		count = 1
		pendingSince = now
	} else {
		count++
	}

	// Boucle : N rapports consécutifs sur une fenêtre d'au moins M minutes.
	loop := false
	if count >= loopThreshold {
		if t, ok := parseDBTime(pendingSince); ok {
			loop = now.Sub(t) >= time.Duration(loopWindowMins)*time.Minute
		}
	}

	_, err = tx.Exec(`UPDATE agents SET reboot_pending = 1, reboot_pending_since = ?, reboot_requested_by = ?, reboot_loop = ?, reboot_request_count = ? WHERE agent_id = ?`,
		formatDBTime(pendingSince), nullIfEmpty(requester), boolInt(loop), count, agentId)
	return err
}

// boolInt converts a boolean into an integer suitable for a BIT/INTEGER column.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullIfEmpty returns nil for an empty string so a NULL is stored instead of an
// empty value (used for reboot_requested_by).
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// formatDBTime normalises a value read from / intended for a timestamp column
// into the "2006-01-02 15:04:05" text form accepted by both SQLite and MSSQL.
// It is the counterpart of parseDBTime.
func formatDBTime(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format("2006-01-02 15:04:05")
	case string:
		if ts, ok := parseDBTimeString(t); ok {
			return ts.UTC().Format("2006-01-02 15:04:05")
		}
		return t
	case []byte:
		return string(t)
	default:
		return v
	}
}
