package db

import (
	"database/sql"
	"strings"
)

// EnsureDscSettingsSchema ajoute la colonne reregistration_mode à la table
// dsc_settings pour les bases déjà créées (les CREATE TABLE IF NOT EXISTS ne
// modifient pas les tables existantes). Chaque ajout est idempotent.
func EnsureDscSettingsSchema(database *sql.DB, driver string) error {
	if driver == "mssql" || driver == "sqlserver" {
		_, err := database.Exec(`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_settings' AND COLUMN_NAME = 'reregistration_mode') ALTER TABLE dsc_settings ADD reregistration_mode NVARCHAR(20) NOT NULL DEFAULT 'deny'`)
		return err
	}
	_, err := database.Exec(`ALTER TABLE dsc_settings ADD COLUMN reregistration_mode TEXT NOT NULL DEFAULT 'deny'`)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return nil
	}
	return err
}
