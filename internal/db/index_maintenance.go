package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// RunIndexMaintenance optimise les index (et, pour SQLite, compacte optionnellement
// le fichier). Le comportement dépend du driver :
//
//   - SQLite : PRAGMA optimize (ANALYZE + optimisations recommandées), puis
//     REINDEX (reconstruction de tous les index). Si vacuum est vrai, un VACUUM est
//     exécuté en dernier pour récupérer les pages libres — cette opération requiert
//     un accès exclusif au fichier et peut bloquer le pull-server pendant qu'elle
//     tourne : elle reste donc optionnelle (index_maintenance_vacuum, défaut false).
//   - MSSQL  : ALTER INDEX ... REORGANIZE sur chaque index utilisateur. C'est une
//     opération en ligne à faible verrouillage (à préférer à REBUILD, hors ligne),
//     adaptée au partage du pool de connexions avec le serveur HTTP. Le paramètre
//     vacuum est ignoré.
func RunIndexMaintenance(database *sql.DB, driver string, vacuum bool) (string, error) {
	driver = strings.ToLower(driver)
	switch driver {
	case "mssql", "sqlserver":
		return runMSSQLIndexMaintenance(database)
	default:
		return runSQLiteIndexMaintenance(database, vacuum)
	}
}

// runSQLiteIndexMaintenance exécute PRAGMA optimize puis REINDEX (toujours) et,
// si vacuum est vrai, VACUUM (exclusif, optionnel).
func runSQLiteIndexMaintenance(database *sql.DB, vacuum bool) (string, error) {
	// PRAGMA optimize exécute ANALYZE et active les optimisations recommandées.
	// Il ne renvoie pas de jeu de résultats : on l'exécute via Exec.
	if _, err := database.Exec(`PRAGMA optimize;`); err != nil {
		return "", fmt.Errorf("PRAGMA optimize: %w", err)
	}
	// REINDEX reconstruit tous les index de toutes les tables.
	if _, err := database.Exec(`REINDEX;`); err != nil {
		return "", fmt.Errorf("REINDEX: %w", err)
	}

	msg := "Index maintenance: PRAGMA optimize + REINDEX ok"
	if vacuum {
		freeBefore, err := sqliteFreelistCount(database)
		if err != nil {
			freeBefore = -1
		}
		// VACUUM requiert un verrou exclusif : si le pull-server traite des
		// requêtes concurrentes, il peut échouer (SQLITE_BUSY). L'erreur est
		// alors remontée et enregistrée comme un échec de la tâche.
		if _, err := database.Exec(`VACUUM;`); err != nil {
			return "", fmt.Errorf("VACUUM: %w", err)
		}
		freeAfter, err := sqliteFreelistCount(database)
		if err != nil {
			freeAfter = -1
		}
		msg = fmt.Sprintf("Index maintenance: PRAGMA optimize + REINDEX + VACUUM ok, free_pages %d -> %d", freeBefore, freeAfter)
	}
	return msg, nil
}

// runMSSQLIndexMaintenance réorganise (en ligne) chaque index utilisateur.
func runMSSQLIndexMaintenance(database *sql.DB) (string, error) {
	rows, err := database.Query(`
		SELECT s.name AS schema_name, t.name AS table_name, i.name AS index_name
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		WHERE i.type > 0 AND t.is_ms_shipped = 0
		ORDER BY t.name, i.index_id`)
	if err != nil {
		return "", fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()

	type dbIndex struct {
		schema string
		table  string
		name   string
	}
	var indexes []dbIndex
	for rows.Next() {
		var idx dbIndex
		if err := rows.Scan(&idx.schema, &idx.table, &idx.name); err != nil {
			return "", err
		}
		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(indexes) == 0 {
		return "Index maintenance: no user index to reorganize", nil
	}

	for _, idx := range indexes {
		query := fmt.Sprintf(`ALTER INDEX [%s] ON [%s].[%s] REORGANIZE;`, idx.name, idx.schema, idx.table)
		if _, err := database.Exec(query); err != nil {
			return "", fmt.Errorf("REORGANIZE [%s].[%s].[%s]: %w", idx.schema, idx.table, idx.name, err)
		}
	}
	return fmt.Sprintf("Index maintenance: REORGANIZE %d user index(es) (online)", len(indexes)), nil
}

// sqliteFreelistCount renvoie le nombre de pages libres (PRAGMA freelist_count),
// utilisé pour rendre le VACUUM observable dans le message de la tâche.
func sqliteFreelistCount(database *sql.DB) (int, error) {
	var n int
	if err := database.QueryRow(`PRAGMA freelist_count;`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
