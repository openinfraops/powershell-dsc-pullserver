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
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'deleted_at') ALTER TABLE agents ADD deleted_at DATETIME NULL`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'deleted_by') ALTER TABLE agents ADD deleted_by NVARCHAR(200) NULL`,
		} {
			if _, err := database.Exec(stmt); err != nil {
				return err
			}
		}
		return nil
	}

	statements := []string{
		`ALTER TABLE agents ADD COLUMN needs_recompile INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN deleted_at TIMESTAMP NULL`,
		`ALTER TABLE agents ADD COLUMN deleted_by TEXT NULL`,
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
