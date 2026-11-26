package db

import (
	"database/sql"
	"fmt"
)

// PruneOldConfigurationVersions supprime les versions historiques de chaque
// configuration (original_name) au-delà du plafond configuré.
//
// Modèle de versionnement :
//   - la version active a name == original_name (base) ;
//   - les versions antérieures ont original_name == base mais un name versionné
//     (base-<horodatage>).
//
// On conserve toujours la version active (name == original_name) et, pour
// chaque base, les maxKeep-1 versions historiques les plus récentes. Les
// versions historiques plus anciennes sont supprimées.
//
// Retourne le nombre de versions supprimées.
func PruneOldConfigurationVersions(database *sql.DB, maxKeep int) (int64, error) {
	if maxKeep < 1 {
		return 0, fmt.Errorf("maxKeep must be >= 1")
	}

	// ROW_NUMBER() OVER (PARTITION BY original_name ...) est supporté à la fois
	// par SQLite (>= 3.25) et MSSQL : la même requête sert pour les deux drivers.
	// historyLimit = maxKeep - 1 : nombre de versions historiques conservées par base.
	historyLimit := maxKeep - 1

	// Sélection des lignes à supprimer (les versions historiques les plus anciennes).
	// La sous-requête est partagée entre l'UPDATE de déliaison et le DELETE afin que
	// les deux restent synchronisés.
	toDelete := `SELECT id FROM (
	       SELECT id,
	              ROW_NUMBER() OVER (PARTITION BY original_name ORDER BY upload_date DESC, id DESC) AS rn
	       FROM configuration_model
	       WHERE original_name IS NOT NULL AND original_name <> ''
	         AND name <> original_name
	   ) t
	   WHERE t.rn > ?`

	// Les versions sont chaînées via configuration_model.previous_id (clé étrangère
	// auto-référente sur MSSQL : FK__configura__previ__...). Supprimer une version
	// encore référencée par une autre ligne (plus récente, ou par une ligne du même
	// lot) viole la contrainte NO ACTION. On casse donc la chaîne AVANT le DELETE :
	// on remet previous_id à NULL sur toute ligne qui pointe vers une ligne à
	// supprimer (y compris au sein du lot supprimé), puis on supprime.
	tx, err := database.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE configuration_model SET previous_id = NULL WHERE previous_id IN (`+toDelete+`)`, historyLimit); err != nil {
		return 0, err
	}

	result, err := tx.Exec(
		`DELETE FROM configuration_model
		 WHERE original_name IS NOT NULL AND original_name <> ''
		   AND name <> original_name
		   AND id IN (`+toDelete+`)`, historyLimit)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}
