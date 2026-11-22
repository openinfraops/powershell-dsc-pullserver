package db

import (
	"database/sql"
	"strings"
)

// IsCertificateThumbprintRegistered reports whether the given client-certificate
// thumbprint is registered for at least one agent.
//
// The thumbprint is compared directly against agents.certificate_thumbprint
// (which is stored in lowercase), so no LOWER() is applied to the column: this
// keeps the comparison index-friendly. SQLite is case-sensitive, so callers must
// pass a lowercase thumbprint; MSSQL's default case-insensitive collation makes
// the exact comparison safe either way.
func IsCertificateThumbprintRegistered(database *sql.DB, thumbprint string) (bool, error) {
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM agents WHERE certificate_thumbprint = ?", thumbprint).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// NormalizeCertificateThumbprints lowercases every agents.certificate_thumbprint
// in the database. Since the registration now stores the thumbprint in lowercase
// and the lookup compares it directly (index-friendly, no LOWER() on the column),
// nodes registered before that change may hold uppercase thumbprints. On SQLite
// (case-sensitive '=') this would break the middleware lookup, so the stored
// values are normalised once at startup. On MSSQL the default case-insensitive
// collation already matches, and the cert↔node binding comparison is done with
// strings.EqualFold, so a no-op here is harmless.
//
// It returns the number of rows that were rewritten.
func NormalizeCertificateThumbprints(database *sql.DB, driver string) (int64, error) {
	driver = strings.ToLower(driver)
	var (
		result sql.Result
		err    error
	)
	if driver == "mssql" || driver == "sqlserver" {
		result, err = database.Exec(`UPDATE agents SET certificate_thumbprint = LOWER(certificate_thumbprint) WHERE certificate_thumbprint IS NOT NULL AND certificate_thumbprint <> LOWER(certificate_thumbprint)`)
	} else {
		result, err = database.Exec(`UPDATE agents SET certificate_thumbprint = lower(certificate_thumbprint) WHERE certificate_thumbprint IS NOT NULL AND certificate_thumbprint <> lower(certificate_thumbprint)`)
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
