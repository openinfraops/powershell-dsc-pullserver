package db

import (
	"database/sql"
	"strings"
	"time"
)

// CountAgents returns the number of non-deleted (active) agents.
func CountAgents(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM agents WHERE deleted_at IS NULL`).Scan(&n)
	return n, err
}

// AgentsByState returns the number of non-deleted agents grouped by their
// compliance state (agents.state). A NULL state is reported under the empty
// key, so callers can map it to an "unknown" label.
func AgentsByState(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT COALESCE(state, '') AS state, COUNT(*) AS c FROM agents WHERE deleted_at IS NULL GROUP BY COALESCE(state, '')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var state string
		var c int
		if err := rows.Scan(&state, &c); err != nil {
			return nil, err
		}
		out[state] = c
	}
	return out, rows.Err()
}

// StalestLastReportAge returns, in seconds, the age of the oldest
// last_communication among non-deleted agents. It returns 0 when there is no
// usable timestamp (no node, or only non-parseable sentinel values such as the
// pre-enrollment placeholders).
func StalestLastReportAge(db *sql.DB, now time.Time) (int64, error) {
	var last interface{}
	err := db.QueryRow(`SELECT MIN(last_communication) FROM agents WHERE deleted_at IS NULL AND last_communication IS NOT NULL`).Scan(&last)
	if err != nil {
		return 0, err
	}
	ts, ok := parseDBTime(last)
	if !ok {
		return 0, nil
	}
	age := now.Sub(ts)
	if age < 0 {
		age = 0
	}
	return int64(age.Seconds()), nil
}

// CountRunningSchedulerTasks returns the number of scheduler tasks currently
// marked as running (last_status = 'running').
func CountRunningSchedulerTasks(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM scheduler_tasks WHERE last_status = 'running'`).Scan(&n)
	return n, err
}

// CountRebootPending returns the number of non-deleted agents with a pending
// reboot request (derived state from the latest report).
func CountRebootPending(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM agents WHERE deleted_at IS NULL AND reboot_pending = 1`).Scan(&n)
	return n, err
}

// CountRebootLoop returns the number of non-deleted agents currently flagged as
// being in a reboot loop.
func CountRebootLoop(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM agents WHERE deleted_at IS NULL AND reboot_loop = 1`).Scan(&n)
	return n, err
}

// parseDBTime converts a value read from a DB timestamp column (SQLite stores
// them as TEXT "2006-01-02 15:04:05", MSSQL returns a time.Time) into a UTC
// time.Time. Sentinel values (pre-enrollment placeholders such as
// "0000-00-01 00:00:00" or "1753-01-01 00:00:00") are rejected, so they do not
// skew staleness metrics.
func parseDBTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() || t.Year() < 2000 {
			return time.Time{}, false
		}
		return t.UTC(), true
	case []byte:
		return parseDBTimeString(string(t))
	case string:
		return parseDBTimeString(t)
	default:
		return time.Time{}, false
	}
}

func parseDBTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			if t.Year() < 2000 {
				return time.Time{}, false
			}
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
