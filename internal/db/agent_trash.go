package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SoftDeleteAgent place l'agent dans la corbeille : la ligne est conservée mais
// marquée deleted_at/deleted_by. La configuration n'est plus servie et le nœud
// disparaît des listes actives.
func SoftDeleteAgent(database *sql.DB, driver, agentId, deletedBy string) error {
	_, err := database.Exec(`UPDATE agents SET deleted_at = ?, deleted_by = ? WHERE agent_id = ?`, time.Now().UTC(), deletedBy, agentId)
	return err
}

// RestoreAgent sort un agent de la corbeille en effaçant deleted_at/deleted_by.
func RestoreAgent(database *sql.DB, driver, agentId string) error {
	_, err := database.Exec(`UPDATE agents SET deleted_at = NULL, deleted_by = NULL WHERE agent_id = ?`, agentId)
	return err
}

// PurgeDeletedAgents supprime définitivement les agents présents en corbeille
// depuis plus de retentionDays jours, avec leurs dépendances (tags, rapports,
// configurations, IP, propriétés). Retourne le nombre d'agents purgés.
func PurgeDeletedAgents(database *sql.DB, driver string, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, fmt.Errorf("retentionDays must be > 0")
	}

	driver = strings.ToLower(driver)
	var ids []string
	var err error

	if driver == "mssql" || driver == "sqlserver" {
		ids, err = collectDeletedAgentIDs(database, `SELECT agent_id FROM agents WHERE deleted_at IS NOT NULL AND deleted_at < DATEADD(day, -?, `+NowExpr(driver)+`)`, retentionDays)
	} else {
		modifier := fmt.Sprintf("-%d days", retentionDays)
		ids, err = collectDeletedAgentIDs(database, `SELECT agent_id FROM agents WHERE deleted_at IS NOT NULL AND datetime(deleted_at) < datetime('now', ?)`, modifier)
	}
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := database.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var purged int64
	for _, id := range ids {
		for _, q := range []string{
			`DELETE FROM agent_tags WHERE agent_id = ?`,
			`DELETE FROM reports WHERE agent_id = ?`,
			`DELETE FROM agent_configurations WHERE agent_id = ?`,
			`DELETE FROM agent_ips WHERE agent_id = ?`,
			`DELETE FROM node_properties WHERE node_id = ?`,
		} {
			if _, err := tx.Exec(q, id); err != nil {
				return purged, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM agents WHERE agent_id = ?`, id); err != nil {
			return purged, err
		}
		purged++
	}

	if err := tx.Commit(); err != nil {
		return purged, err
	}
	return purged, nil
}

func collectDeletedAgentIDs(database *sql.DB, query string, arg interface{}) ([]string, error) {
	rows, err := database.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
