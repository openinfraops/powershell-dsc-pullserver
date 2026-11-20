package db

import (
	"database/sql"
	"fmt"
	"time"
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
	result, err := database.Exec(
		`DELETE FROM configuration_model
		 WHERE original_name IS NOT NULL AND original_name <> ''
		   AND name <> original_name
		   AND id IN (
		       SELECT id FROM (
		           SELECT id,
		                  ROW_NUMBER() OVER (PARTITION BY original_name ORDER BY upload_date DESC, id DESC) AS rn
		           FROM configuration_model
		           WHERE original_name IS NOT NULL AND original_name <> ''
		             AND name <> original_name
		       ) t
		       WHERE t.rn > ?
		   )`, historyLimit)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// StartConfigurationVersionCleanupWorker lance le nettoyage immédiatement puis
// à intervalle fixe.
func StartConfigurationVersionCleanupWorker(database *sql.DB, maxKeep int, interval time.Duration, onRunStart func(startedAt time.Time, nextRunAt *time.Time), onResult func(startedAt time.Time, pruned int64, err error)) {
	run := func() {
		startedAt := time.Now().UTC()
		var nextRunAt *time.Time
		if interval > 0 {
			n := startedAt.Add(interval)
			nextRunAt = &n
		}
		if onRunStart != nil {
			onRunStart(startedAt, nextRunAt)
		}
		pruned, err := PruneOldConfigurationVersions(database, maxKeep)
		if onResult != nil {
			onResult(startedAt, pruned, err)
		}
	}

	run()
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			run()
		}
	}()
}
