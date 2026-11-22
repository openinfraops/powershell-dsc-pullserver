package main

import (
	"database/sql"
	"time"

	"go-dsc-pull/internal/db"
)

// taskMeta describes a scheduled background task: its scheduler key/display name,
// whether it is currently enabled, and its configured interval. It centralises
// the schedule computation and the run begin/complete recording that were
// repeated for every task in runApp.
type taskMeta struct {
	key      string
	display  string
	enabled  bool
	interval time.Duration
}

// intervalFromMins converts a configured interval in minutes to a duration,
// falling back to def when the value is <= 0.
func intervalFromMins(mins, def int) time.Duration {
	if mins <= 0 {
		mins = def
	}
	return time.Duration(mins) * time.Minute
}

// firstRun returns the first scheduled run time, or nil when the task is
// disabled. The task is still registered (with a nil next run) in that case so
// the Scheduler page shows the task as inactive.
func (t taskMeta) firstRun() *time.Time {
	if !t.enabled {
		return nil
	}
	n := time.Now().UTC().Add(t.interval)
	return &n
}

// register upserts the scheduler task row.
func (t taskMeta) register(dbConn *sql.DB, driver string) {
	_ = db.UpsertSchedulerTask(dbConn, driver, t.key, t.display, t.firstRun())
}

// beginRecord returns a callback that records the start of a task run.
func (t taskMeta) beginRecord(dbConn *sql.DB, driver string) func(time.Time, *time.Time) {
	return func(startedAt time.Time, nextRunAt *time.Time) {
		_ = db.BeginSchedulerTaskRun(dbConn, driver, t.key, t.display, "automatic", startedAt, nextRunAt)
	}
}

// completeRecord returns a callback that records the outcome of a task run
// (success or error) and its next scheduled time.
func (t taskMeta) completeRecord(dbConn *sql.DB, driver string) func(startedAt time.Time, msg string, runErr error) {
	return func(startedAt time.Time, msg string, runErr error) {
		next := startedAt.Add(t.interval)
		status := "success"
		if runErr != nil {
			status = "error"
		}
		_ = db.CompleteSchedulerTaskRun(dbConn, driver, t.key, status, msg, time.Now().UTC(), &next)
	}
}
