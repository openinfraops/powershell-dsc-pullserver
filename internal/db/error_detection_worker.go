package db

import (
	"context"
	"database/sql"
	"time"

	"go-dsc-pull/internal/logs"
)

// StartErrorDetectionWorker runs the detection task immediately and then at a
// fixed interval. Each detection that is alert-worthy (and not dedup-suppressed)
// is written to the log file (the audit log) and reported through onResult.
// It stops cleanly when ctx is cancelled.
func StartErrorDetectionWorker(ctx context.Context, database *sql.DB, p ErrorDetectionParams, interval time.Duration, onRunStart func(startedAt time.Time, nextRunAt *time.Time), onResult func(startedAt time.Time, alerted int, err error)) {
	run := func() {
		startedAt := time.Now().UTC()
		var nextRunAt *time.Time
		if interval > 0 {
			n := startedAt.Add(interval)
			nextRunAt = &n
		}
		if onRunStart != nil {
			onRunStart(startedAt, nextRunAt)
		}
		results, err := RunErrorDetection(database, p)
		alerted := 0
		if err == nil {
			for _, r := range results {
				if r.Alerted {
					alerted++
					logs.WriteLogFile("ALERT [ERROR DETECTION] " + r.Message)
				}
			}
		}
		if onResult != nil {
			onResult(startedAt, alerted, err)
		}
	}

	run()
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
