package db

import (
	"database/sql"
	"strings"
)

// EnsureAgentSchema ajoute les colonnes manquantes à la table agents pour les
// bases déjà créées (les CREATE TABLE IF NOT EXISTS ne modifient pas les tables
// existantes). Chaque ajout est idempotent : on ignore l'erreur si la colonne
// existe déjà.
func EnsureAgentSchema(database *sql.DB, driver string) error {
	if driver == "mssql" || driver == "sqlserver" {
		for _, stmt := range []string{
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'needs_recompile') ALTER TABLE agents ADD needs_recompile BIT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'force_cert_rotation') ALTER TABLE agents ADD force_cert_rotation BIT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'deleted_at') ALTER TABLE agents ADD deleted_at DATETIME NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'deleted_by') ALTER TABLE agents ADD deleted_by NVARCHAR(200) NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'certificate_public_key') ALTER TABLE agents ADD certificate_public_key NVARCHAR(MAX) NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'certificate_key_algorithm') ALTER TABLE agents ADD certificate_key_algorithm NVARCHAR(16) NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reboot_pending') ALTER TABLE agents ADD reboot_pending BIT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reboot_pending_since') ALTER TABLE agents ADD reboot_pending_since DATETIME NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reboot_requested_by') ALTER TABLE agents ADD reboot_requested_by NVARCHAR(200) NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reboot_loop') ALTER TABLE agents ADD reboot_loop BIT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reboot_request_count') ALTER TABLE agents ADD reboot_request_count INT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'persistent') ALTER TABLE agents ADD persistent BIT NOT NULL DEFAULT 0`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'reregistration_window_until') ALTER TABLE agents ADD reregistration_window_until DATETIME NULL`,
		} {
			if _, err := database.Exec(stmt); err != nil {
				return err
			}
		}
		return nil
	}

	statements := []string{
		`ALTER TABLE agents ADD COLUMN needs_recompile INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN force_cert_rotation INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN deleted_at TIMESTAMP NULL`,
		`ALTER TABLE agents ADD COLUMN deleted_by TEXT NULL`,
		`ALTER TABLE agents ADD COLUMN certificate_public_key TEXT NULL`,
		`ALTER TABLE agents ADD COLUMN certificate_key_algorithm TEXT NULL`,
		`ALTER TABLE agents ADD COLUMN reboot_pending INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN reboot_pending_since TIMESTAMP NULL`,
		`ALTER TABLE agents ADD COLUMN reboot_requested_by TEXT NULL`,
		`ALTER TABLE agents ADD COLUMN reboot_loop INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN reboot_request_count INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN persistent INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN reregistration_window_until TIMESTAMP NULL`,
	}
	for _, stmt := range statements {
		if _, err := database.Exec(stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}
