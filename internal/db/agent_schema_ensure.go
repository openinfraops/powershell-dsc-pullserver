package db

import (
	"database/sql"
	"strings"
)

// EnsureAgentSchema ajoute les colonnes manquantes à la table agents pour les
// bases déjà créées (les CREATE TABLE IF NOT EXISTS ne modifient pas les tables
// existantes). Retourne nil si la colonne existe déjà.
func EnsureAgentSchema(database *sql.DB, driver string) error {
	if driver == "mssql" || driver == "sqlserver" {
		_, err := database.Exec(`IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'agents' AND COLUMN_NAME = 'needs_recompile') ALTER TABLE agents ADD needs_recompile BIT NOT NULL DEFAULT 0`)
		return err
	}
	_, err := database.Exec(`ALTER TABLE agents ADD COLUMN needs_recompile INTEGER DEFAULT 0`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}
