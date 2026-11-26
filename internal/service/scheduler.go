package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go-dsc-pull/internal/logs"
)

// ScheduledTask describes one recurring background task. Run performs a single
// execution and returns a human-readable outcome message (stored in the
// scheduler_tasks run history) and an error (nil on success). The scheduler
// records the begin/complete lifecycle and re-runs Run every Interval until the
// context is cancelled.
type ScheduledTask struct {
	Key         string
	DisplayName string
	Enabled     bool
	Interval    time.Duration
	Run         func(dbConn *sql.DB, driver string) (message string, err error)
}

// TaskRecorder persists scheduler task and run lifecycle events. The
// application supplies an implementation backed by the configured store so this
// package does not depend on the storage layer.
type TaskRecorder interface {
	// UpsertTask registers (or updates) a scheduler task and its next run time.
	UpsertTask(taskName, displayName string, nextRunAt *time.Time) error
	// BeginRun records the start of a run.
	BeginRun(taskName, displayName, triggerSource string, startedAt time.Time, nextRunAt *time.Time) error
	// CompleteRun records the outcome of a run and the next scheduled time.
	CompleteRun(taskName, status, message string, finishedAt time.Time, nextRunAt *time.Time) error
}

// firstRunTime returns the first scheduled run time for a task, or nil when the
// task is disabled (it is still registered so the Scheduler page shows it as
// inactive).
func firstRunTime(t ScheduledTask) *time.Time {
	if !t.Enabled {
		return nil
	}
	n := time.Now().UTC().Add(t.Interval)
	return &n
}

// StartScheduledTasks registers every task and launches a run loop for each
// enabled task. Tasks run immediately, then at their Interval until ctx is
// cancelled. Disabled tasks are registered but never run. The scheduler_tasks
// schema is unchanged.
func StartScheduledTasks(ctx context.Context, dbConn *sql.DB, driver string, recorder TaskRecorder, tasks []ScheduledTask) {
	for i := range tasks {
		t := &tasks[i]
		if err := recorder.UpsertTask(t.Key, t.DisplayName, firstRunTime(*t)); err != nil {
			logs.WriteLogFile(fmt.Sprintf("WARN [SCHEDULER] Failed to register scheduler task %q: %v", t.Key, err))
		}
		if !t.Enabled {
			continue
		}
		go runScheduledTaskLoop(ctx, dbConn, driver, recorder, *t)
	}
}

// runScheduledTaskLoop runs a single task immediately, then at every interval,
// recording each run's begin/complete. It stops when ctx is cancelled. Runs are
// sequential, so a long-running task simply delays the next tick rather than
// overlapping itself.
func runScheduledTaskLoop(ctx context.Context, dbConn *sql.DB, driver string, recorder TaskRecorder, t ScheduledTask) {
	run := func() {
		startedAt := time.Now().UTC()
		var nextRunAt *time.Time
		if t.Interval > 0 {
			n := startedAt.Add(t.Interval)
			nextRunAt = &n
		}
		if err := recorder.BeginRun(t.Key, t.DisplayName, "automatic", startedAt, nextRunAt); err != nil {
			logs.WriteLogFile(fmt.Sprintf("WARN [SCHEDULER] Failed to record the start of task %q: %v", t.Key, err))
		}

		message, runErr := t.Run(dbConn, driver)
		if runErr != nil {
			message = runErr.Error()
		}

		next := startedAt.Add(t.Interval)
		status := "success"
		if runErr != nil {
			status = "error"
		}
		if err := recorder.CompleteRun(t.Key, status, message, time.Now().UTC(), &next); err != nil {
			logs.WriteLogFile(fmt.Sprintf("WARN [SCHEDULER] Failed to record the completion of task %q: %v", t.Key, err))
		}
	}

	run()
	if t.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
