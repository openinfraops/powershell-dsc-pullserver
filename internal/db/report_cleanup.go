package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CleanupOldReports deletes reports older than the retention window.
func CleanupOldReports(database *sql.DB, driver string, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, fmt.Errorf("retentionDays must be > 0")
	}

	driver = strings.ToLower(driver)
	var (
		result sql.Result
		err    error
	)

	switch driver {
	case "mssql", "sqlserver":
		result, err = database.Exec(`DELETE FROM reports WHERE created_at < DATEADD(day, -?, GETDATE())`, retentionDays)
	default:
		modifier := fmt.Sprintf("-%d days", retentionDays)
		result, err = database.Exec(`DELETE FROM reports WHERE datetime(created_at) < datetime('now', ?)`, modifier)
	}
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// StartReportCleanupWorker runs cleanup immediately and then at a fixed interval.
// It stops cleanly when ctx is cancelled (arrêt propre du service).
func StartReportCleanupWorker(ctx context.Context, database *sql.DB, driver string, retentionDays int, interval time.Duration, onRunStart func(startedAt time.Time, nextRunAt *time.Time), onResult func(startedAt time.Time, deleted int64, err error)) {
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
		deleted, err := CleanupOldReports(database, driver, retentionDays)
		if onResult != nil {
			onResult(startedAt, deleted, err)
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
