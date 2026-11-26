package db

import (
	"database/sql"
	"fmt"
	"strings"
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
		result, err = database.Exec(`DELETE FROM reports WHERE created_at < DATEADD(day, -?, `+NowExpr(driver)+`)`, retentionDays)
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
