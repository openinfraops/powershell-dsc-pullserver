package db

import (
	"database/sql"
	"strings"
	"time"
)

type ReleaseCheckState struct {
	LatestRelease    string
	LatestReleaseURL string
	UpdateAvailable  bool
	ReleaseCheckOK   bool
	ReleaseCheckedAt *string
}

func PersistReleaseCheckSuccess(db *sql.DB, driverName string, latestRelease, latestReleaseURL string, updateAvailable bool) error {
	if driverName == "mssql" || driverName == "sqlserver" {
		_, err := db.Exec(`UPDATE dsc_infra_info SET latest_release = ?, latest_release_url = ?, update_available = ?, release_check_ok = 1, release_checked_at = `+NowExpr(driverName)+` WHERE id = 1`,
			latestRelease, latestReleaseURL, updateAvailable)
		return err
	}
	_, err := db.Exec(`UPDATE dsc_infra_info SET latest_release = ?, latest_release_url = ?, update_available = ?, release_check_ok = 1, release_checked_at = ? WHERE id = 1`,
		latestRelease, latestReleaseURL, updateAvailable, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

func PersistReleaseCheckFailure(db *sql.DB, driverName string) error {
	if driverName == "mssql" || driverName == "sqlserver" {
		_, err := db.Exec(`UPDATE dsc_infra_info SET release_check_ok = 0, release_checked_at = `+NowExpr(driverName)+` WHERE id = 1`)
		return err
	}
	_, err := db.Exec(`UPDATE dsc_infra_info SET release_check_ok = 0, release_checked_at = ? WHERE id = 1`, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

func GetReleaseCheckState(db *sql.DB) (ReleaseCheckState, error) {
	row := db.QueryRow(`SELECT latest_release, latest_release_url, update_available, release_check_ok, release_checked_at FROM dsc_infra_info WHERE id = 1`)

	var (
		latestRel sql.NullString
		latestURL sql.NullString
		updateAny interface{}
		checkAny  interface{}
		checkedAt sql.NullString
	)
	if err := row.Scan(&latestRel, &latestURL, &updateAny, &checkAny, &checkedAt); err != nil {
		return ReleaseCheckState{}, err
	}

	state := ReleaseCheckState{}
	if latestRel.Valid {
		state.LatestRelease = latestRel.String
	}
	if latestURL.Valid {
		state.LatestReleaseURL = latestURL.String
	}
	state.UpdateAvailable = coerceBool(updateAny)
	state.ReleaseCheckOK = coerceBool(checkAny)
	if checkedAt.Valid {
		state.ReleaseCheckedAt = &checkedAt.String
	}
	return state, nil
}

func coerceBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int32:
		return t != 0
	case int:
		return t != 0
	case []byte:
		s := strings.TrimSpace(strings.ToLower(string(t)))
		return s == "1" || s == "true"
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true"
	default:
		return false
	}
}
