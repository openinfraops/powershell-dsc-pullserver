package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type SchedulerTaskInfo struct {
	TaskName    string  `json:"task_name"`
	DisplayName string  `json:"display_name"`
	NextRunAt   *string `json:"next_run_at,omitempty"`
	LastRunAt   *string `json:"last_run_at,omitempty"`
	LastStatus  string  `json:"last_status"`
	LastMessage string  `json:"last_message,omitempty"`
}

type SchedulerTaskRun struct {
	TaskName      string  `json:"task_name"`
	StartedAt     string  `json:"started_at"`
	FinishedAt    *string `json:"finished_at,omitempty"`
	Status        string  `json:"status"`
	Message       string  `json:"message,omitempty"`
	TriggerSource string  `json:"trigger_source"`
}

func formatTS(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func anyToString(v interface{}) (string, bool) {
	if v == nil {
		return "", false
	}
	switch t := v.(type) {
	case time.Time:
		return formatTS(t), true
	case []byte:
		s := strings.TrimSpace(string(t))
		if s == "" {
			return "", false
		}
		return s, true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "", false
		}
		return s, true
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", t))
		if s == "" || s == "<nil>" {
			return "", false
		}
		return s, true
	}
}

func UpsertSchedulerTask(db *sql.DB, driver, taskName, displayName string, nextRunAt *time.Time) error {
	var nextAny interface{}
	if nextRunAt != nil {
		nextAny = formatTS(*nextRunAt)
	}
	nowExpr := NowExpr(driver)
	if driver == "mssql" || driver == "sqlserver" {
		_, err := db.Exec(`
IF EXISTS (SELECT 1 FROM scheduler_tasks WHERE task_name = ?)
BEGIN
	UPDATE scheduler_tasks SET display_name = ?, next_run_at = ?, updated_at = `+nowExpr+` WHERE task_name = ?
END
ELSE
BEGIN
	INSERT INTO scheduler_tasks (task_name, display_name, next_run_at, last_status, updated_at) VALUES (?, ?, ?, 'idle', `+nowExpr+`)
END`, taskName, displayName, nextAny, taskName, taskName, displayName, nextAny)
		return err
	}
	_, err := db.Exec(`
INSERT INTO scheduler_tasks (task_name, display_name, next_run_at, last_status, updated_at)
VALUES (?, ?, ?, 'idle', `+nowExpr+`)
ON CONFLICT(task_name) DO UPDATE SET
	display_name=excluded.display_name,
	next_run_at=excluded.next_run_at,
	updated_at=`+nowExpr+``, taskName, displayName, nextAny)
	return err
}

func BeginSchedulerTaskRun(db *sql.DB, driver, taskName, displayName, triggerSource string, startedAt time.Time, nextRunAt *time.Time) error {
	if err := UpsertSchedulerTask(db, driver, taskName, displayName, nextRunAt); err != nil {
		return err
	}
	started := formatTS(startedAt)
	var nextAny interface{}
	if nextRunAt != nil {
		nextAny = formatTS(*nextRunAt)
	}
	nowExpr := NowExpr(driver)
	if driver == "mssql" || driver == "sqlserver" {
		if _, err := db.Exec(`UPDATE scheduler_tasks SET last_run_at = ?, next_run_at = ?, last_status = 'running', last_message = NULL, updated_at = `+nowExpr+` WHERE task_name = ?`, started, nextAny, taskName); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO scheduler_task_runs (task_name, started_at, status, trigger_source, created_at) VALUES (?, ?, 'running', ?, `+nowExpr+`)`, taskName, started, triggerSource)
		return err
	}
	if _, err := db.Exec(`UPDATE scheduler_tasks SET last_run_at = ?, next_run_at = ?, last_status = 'running', last_message = NULL, updated_at = `+nowExpr+` WHERE task_name = ?`, started, nextAny, taskName); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO scheduler_task_runs (task_name, started_at, status, trigger_source, created_at) VALUES (?, ?, 'running', ?, `+nowExpr+`)`, taskName, started, triggerSource)
	return err
}

func CompleteSchedulerTaskRun(db *sql.DB, driver, taskName, status, message string, finishedAt time.Time, nextRunAt *time.Time) error {
	finished := formatTS(finishedAt)
	nowExpr := NowExpr(driver)
	var nextAny interface{}
	if nextRunAt != nil {
		nextAny = formatTS(*nextRunAt)
	}
	if driver == "mssql" || driver == "sqlserver" {
		if _, err := db.Exec(`
WITH latest AS (
	SELECT TOP (1) id FROM scheduler_task_runs WHERE task_name = ? AND status = 'running' ORDER BY started_at DESC
)
UPDATE scheduler_task_runs SET finished_at = ?, status = ?, message = ? WHERE id IN (SELECT id FROM latest)
`, taskName, finished, status, message); err != nil {
			return err
		}
		_, err := db.Exec(`UPDATE scheduler_tasks SET next_run_at = ?, last_status = ?, last_message = ?, updated_at = `+nowExpr+` WHERE task_name = ?`, nextAny, status, message, taskName)
		return err
	}
	if _, err := db.Exec(`
UPDATE scheduler_task_runs
SET finished_at = ?, status = ?, message = ?
WHERE id = (
	SELECT id FROM scheduler_task_runs WHERE task_name = ? AND status = 'running' ORDER BY started_at DESC LIMIT 1
)`, finished, status, message, taskName); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE scheduler_tasks SET next_run_at = ?, last_status = ?, last_message = ?, updated_at = `+nowExpr+` WHERE task_name = ?`, nextAny, status, message, taskName)
	return err
}

func ListSchedulerTasks(db *sql.DB) ([]SchedulerTaskInfo, error) {
	rows, err := db.Query(`SELECT task_name, display_name, next_run_at, last_run_at, last_status, last_message FROM scheduler_tasks ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SchedulerTaskInfo, 0)
	for rows.Next() {
		var t SchedulerTaskInfo
		var nextAny, lastAny, msgAny interface{}
		if err := rows.Scan(&t.TaskName, &t.DisplayName, &nextAny, &lastAny, &t.LastStatus, &msgAny); err != nil {
			return nil, err
		}
		if s, ok := anyToString(nextAny); ok {
			t.NextRunAt = &s
		}
		if s, ok := anyToString(lastAny); ok {
			t.LastRunAt = &s
		}
		if s, ok := anyToString(msgAny); ok {
			t.LastMessage = s
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func ListSchedulerTaskRuns(db *sql.DB, taskName string, limit int) ([]SchedulerTaskRun, error) {
	if limit <= 0 {
		limit = 50
	}
	query := fmt.Sprintf(`SELECT task_name, started_at, finished_at, status, message, trigger_source FROM scheduler_task_runs WHERE task_name = ? ORDER BY started_at DESC LIMIT %d`, limit)
	rows, err := db.Query(query, taskName)
	if err != nil {
		// MSSQL fallback (no LIMIT)
		queryMSSQL := fmt.Sprintf(`SELECT TOP (%d) task_name, started_at, finished_at, status, message, trigger_source FROM scheduler_task_runs WHERE task_name = ? ORDER BY started_at DESC`, limit)
		rows, err = db.Query(queryMSSQL, taskName)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	out := make([]SchedulerTaskRun, 0)
	for rows.Next() {
		var r SchedulerTaskRun
		var startedAny, finishedAny, msgAny interface{}
		if err := rows.Scan(&r.TaskName, &startedAny, &finishedAny, &r.Status, &msgAny, &r.TriggerSource); err != nil {
			return nil, err
		}
		if s, ok := anyToString(startedAny); ok {
			r.StartedAt = s
		}
		if s, ok := anyToString(finishedAny); ok {
			r.FinishedAt = &s
		}
		if s, ok := anyToString(msgAny); ok {
			r.Message = s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListSchedulerTaskRunsPage returns a paginated list of task runs ordered by newest first.
func ListSchedulerTaskRunsPage(db *sql.DB, driver, taskName string, offset, limit int) ([]SchedulerTaskRun, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	driver = strings.ToLower(driver)
	var (
		rows *sql.Rows
		err  error
	)

	if driver == "mssql" || driver == "sqlserver" {
		rows, err = db.Query(`
SELECT task_name, started_at, finished_at, status, message, trigger_source
FROM scheduler_task_runs
WHERE task_name = ?
ORDER BY started_at DESC
OFFSET ? ROWS FETCH NEXT ? ROWS ONLY`, taskName, offset, limit)
	} else {
		rows, err = db.Query(`
SELECT task_name, started_at, finished_at, status, message, trigger_source
FROM scheduler_task_runs
WHERE task_name = ?
ORDER BY started_at DESC
LIMIT ? OFFSET ?`, taskName, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SchedulerTaskRun, 0)
	for rows.Next() {
		var r SchedulerTaskRun
		var startedAny, finishedAny, msgAny interface{}
		if err := rows.Scan(&r.TaskName, &startedAny, &finishedAny, &r.Status, &msgAny, &r.TriggerSource); err != nil {
			return nil, err
		}
		if s, ok := anyToString(startedAny); ok {
			r.StartedAt = s
		}
		if s, ok := anyToString(finishedAny); ok {
			r.FinishedAt = &s
		}
		if s, ok := anyToString(msgAny); ok {
			r.Message = s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CancelRunningSchedulerTasks marks stale running scheduler executions as cancelled.
// This is used on app startup to close runs left in running state after an unclean stop.
func CancelRunningSchedulerTasks(db *sql.DB, driver string, cancelledAt time.Time, message string) (int64, error) {
	if strings.TrimSpace(message) == "" {
		message = "Cancelled on application startup"
	}

	finished := formatTS(cancelledAt)
	finishedMSSQL := cancelledAt.UTC()
	nowExpr := NowExpr(driver)
	driver = strings.ToLower(driver)

	var result sql.Result
	var err error
	if driver == "mssql" || driver == "sqlserver" {
		result, err = db.Exec(`
UPDATE scheduler_task_runs
SET finished_at = ?, status = 'cancelled', message = CASE WHEN message IS NULL OR LTRIM(RTRIM(message)) = '' THEN ? ELSE message END
WHERE status = 'running'
`, finishedMSSQL, message)
	} else {
		result, err = db.Exec(`
UPDATE scheduler_task_runs
SET finished_at = ?, status = 'cancelled', message = CASE WHEN message IS NULL OR TRIM(message) = '' THEN ? ELSE message END
WHERE status = 'running'
`, finished, message)
	}
	if err != nil {
		return 0, err
	}

	cancelledCount, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if cancelledCount > 0 {
		if driver == "mssql" || driver == "sqlserver" {
			_, err = db.Exec(`
UPDATE scheduler_tasks
SET last_status = 'cancelled', last_message = ?, updated_at = `+nowExpr+`
WHERE last_status = 'running'
`, message)
		} else {
			_, err = db.Exec(`
UPDATE scheduler_tasks
SET last_status = 'cancelled', last_message = ?, updated_at = `+nowExpr+`
WHERE last_status = 'running'
`, message)
		}
		if err != nil {
			return cancelledCount, err
		}
	}

	return cancelledCount, nil
}
