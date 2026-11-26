package service

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"
)

// fakeTaskRecorder records scheduler lifecycle calls for assertions.
type fakeTaskRecorder struct {
	mu        sync.Mutex
	upserts   []taskUpsert
	begins    []runBegin
	completes []runComplete
}

type taskUpsert struct {
	name    string
	display string
	next    *time.Time
}

type runBegin struct {
	name    string
	display string
	trigger string
	next    *time.Time
}

type runComplete struct {
	name     string
	status   string
	message  string
	finished time.Time
	next     *time.Time
}

func (f *fakeTaskRecorder) UpsertTask(taskName, displayName string, nextRunAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts = append(f.upserts, taskUpsert{name: taskName, display: displayName, next: nextRunAt})
	return nil
}

func (f *fakeTaskRecorder) BeginRun(taskName, displayName, triggerSource string, startedAt time.Time, nextRunAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins = append(f.begins, runBegin{name: taskName, display: displayName, trigger: triggerSource, next: nextRunAt})
	return nil
}

func (f *fakeTaskRecorder) CompleteRun(taskName, status, message string, finishedAt time.Time, nextRunAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completes = append(f.completes, runComplete{name: taskName, status: status, message: message, finished: finishedAt, next: nextRunAt})
	return nil
}

func (f *fakeTaskRecorder) upsertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.upserts)
}

func (f *fakeTaskRecorder) completeFor(name string) (runComplete, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.completes {
		if c.name == name {
			return c, true
		}
	}
	return runComplete{}, false
}

func (f *fakeTaskRecorder) beginFor(name string) (runBegin, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.begins {
		if b.name == name {
			return b, true
		}
	}
	return runBegin{}, false
}

func TestFirstRunTime(t *testing.T) {
	enabled := ScheduledTask{Enabled: true, Interval: time.Hour}
	if n := firstRunTime(enabled); n == nil {
		t.Fatalf("enabled task should have a next run time")
	} else if n.Before(time.Now().UTC()) {
		t.Fatalf("next run should be in the future, got %v", n)
	}

	disabled := ScheduledTask{Enabled: false, Interval: time.Hour}
	if n := firstRunTime(disabled); n != nil {
		t.Fatalf("disabled task should have a nil next run, got %v", n)
	}
}

func TestStartScheduledTasks(t *testing.T) {
	rec := &fakeTaskRecorder{}
	ranCh := make(chan struct{}, 1)
	var disabledRan bool

	tasks := []ScheduledTask{
		{
			Key:         "enabled-task",
			DisplayName: "Enabled task",
			Enabled:     true,
			Interval:    time.Hour,
			Run: func(dbConn *sql.DB, driver string) (string, error) {
				ranCh <- struct{}{}
				return "ok", nil
			},
		},
		{
			Key:         "disabled-task",
			DisplayName: "Disabled task",
			Enabled:     false,
			Interval:    time.Hour,
			Run: func(dbConn *sql.DB, driver string) (string, error) {
				disabledRan = true
				return "should-not-run", nil
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	StartScheduledTasks(ctx, nil, "sqlite", rec, tasks)
	defer cancel()

	// Registration is synchronous: both tasks (enabled + disabled) are upserted.
	if got := rec.upsertCount(); got != 2 {
		t.Fatalf("expected 2 task upserts, got %d", got)
	}

	// The enabled task runs immediately.
	select {
	case <-ranCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("enabled task did not run")
	}

	// The disabled task never runs.
	time.Sleep(50 * time.Millisecond)
	if disabledRan {
		t.Fatalf("disabled task should not have run")
	}

	// The enabled run is recorded as an automatic run with a success message.
	time.Sleep(50 * time.Millisecond)
	begin, ok := rec.beginFor("enabled-task")
	if !ok {
		t.Fatalf("expected a begin record for enabled-task")
	}
	if begin.trigger != "automatic" {
		t.Fatalf("expected automatic trigger, got %q", begin.trigger)
	}
	complete, ok := rec.completeFor("enabled-task")
	if !ok {
		t.Fatalf("expected a completed run for enabled-task")
	}
	if complete.status != "success" {
		t.Fatalf("expected status success, got %q", complete.status)
	}
	if complete.message != "ok" {
		t.Fatalf("expected message 'ok', got %q", complete.message)
	}
}

func TestStartScheduledTasks_RecordsErrorAsMessage(t *testing.T) {
	rec := &fakeTaskRecorder{}
	doneCh := make(chan struct{}, 1)

	tasks := []ScheduledTask{
		{
			Key:         "failing-task",
			DisplayName: "Failing",
			Enabled:     true,
			Interval:    time.Hour,
			Run: func(dbConn *sql.DB, driver string) (string, error) {
				defer func() { doneCh <- struct{}{} }()
				return "", sql.ErrNoRows
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	StartScheduledTasks(ctx, nil, "sqlite", rec, tasks)
	defer cancel()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("failing task did not run")
	}

	time.Sleep(50 * time.Millisecond)
	complete, ok := rec.completeFor("failing-task")
	if !ok {
		t.Fatalf("expected a completed run for failing-task")
	}
	if complete.status != "error" {
		t.Fatalf("expected status error, got %q", complete.status)
	}
	if complete.message != sql.ErrNoRows.Error() {
		t.Fatalf("expected the error text as the message, got %q", complete.message)
	}
}
