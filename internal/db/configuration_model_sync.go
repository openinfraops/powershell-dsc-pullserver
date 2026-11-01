package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go-dsc-pull/internal/service"
)

// ImportCompiledConfigurations stores generated MOFs using the same history contract as web uploads.
func ImportCompiledConfigurations(database *sql.DB, compiled []service.CompiledConfiguration) (int, error) {
	return ImportCompiledConfigurationsAndAssign(database, compiled, nil)
}

func ImportCompiledConfigurationsAndAssign(database *sql.DB, compiled []service.CompiledConfiguration, nodeAgentIDs map[string]string) (int, error) {
	changed := 0
	tx, err := database.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	assignedAgentIDs := make(map[string]bool)
	for _, agentID := range nodeAgentIDs {
		if agentID != "" && !assignedAgentIDs[agentID] {
			if _, err := tx.Exec(`DELETE FROM agent_configurations WHERE agent_id = ?`, agentID); err != nil {
				return 0, err
			}
			assignedAgentIDs[agentID] = true
		}
	}

	for _, config := range compiled {
		name := strings.ToLower(strings.TrimSpace(config.Name))
		if name == "" || len(config.Data) == 0 {
			return 0, fmt.Errorf("invalid compiled configuration")
		}

		var existingID int64
		var existingMof []byte
		var uploadDate string
		err = tx.QueryRow(`SELECT id, mof_file, upload_date FROM configuration_model WHERE name = ?`, name).Scan(&existingID, &existingMof, &uploadDate)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		contentChanged := err == sql.ErrNoRows || string(existingMof) != string(config.Data)
		if contentChanged {
			var previousID interface{}
			if err == nil {
				versionName := name + "-" + time.Now().UTC().Format("20060102_150405.000")
				if _, err := tx.Exec(`UPDATE configuration_model SET name = ? WHERE id = ?`, versionName, existingID); err != nil {
					return 0, err
				}
				previousID = existingID
			}
			if _, err := tx.Exec(`INSERT INTO configuration_model (name, original_name, previous_id, uploaded_by, mof_file) VALUES (?, ?, ?, ?, ?)`, name, name, previousID, "configuration_repo", config.Data); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(`UPDATE agents SET state = 'pending_apply' WHERE agent_id IN (SELECT agent_id FROM agent_configurations WHERE configuration_name = ?)`, name); err != nil {
				return 0, err
			}
			changed++
		}
		if agentID, ok := nodeAgentIDs[name]; ok {
			if _, err := tx.Exec(`INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentID, name); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(`UPDATE agents SET state = 'pending_apply' WHERE agent_id = ?`, agentID); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}
