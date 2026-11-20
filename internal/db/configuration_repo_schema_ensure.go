package db

import (
	"database/sql"
	"strings"
)

// EnsureConfigurationRepoSettingsSchema ajoute les colonnes manquantes à la
// table configuration_repo_settings pour les bases déjà créées (les CREATE TABLE
// IF NOT EXISTS ne modifient pas les tables existantes). Chaque ajout est
// idempotent : on ignore l'erreur si la colonne existe déjà.
func EnsureConfigurationRepoSettingsSchema(database *sql.DB, driver string) error {
	if driver == "mssql" || driver == "sqlserver" {
		for _, stmt := range []string{
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'configuration_repo_settings' AND COLUMN_NAME = 'repository_username') ALTER TABLE configuration_repo_settings ADD repository_username NVARCHAR(255) NOT NULL DEFAULT ''`,
			`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'configuration_repo_settings' AND COLUMN_NAME = 'repository_password') ALTER TABLE configuration_repo_settings ADD repository_password NVARCHAR(2048) NOT NULL DEFAULT ''`,
		} {
			if _, err := database.Exec(stmt); err != nil {
				return err
			}
		}
		return nil
	}

	statements := []string{
		`ALTER TABLE configuration_repo_settings ADD COLUMN repository_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE configuration_repo_settings ADD COLUMN repository_password TEXT NOT NULL DEFAULT ''`,
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
