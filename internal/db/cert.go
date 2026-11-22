package db

import "database/sql"

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
