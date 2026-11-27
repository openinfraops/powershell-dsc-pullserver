package db

import (
	"database/sql"
	"time"
)

// SetAgentPersistent marque ou démarque un agent comme nœud persistant soumis à
// la politique de ré-enregistrement (#87). Un nœud persistant est un serveur
// dont l'identité ne doit pas pouvoir être prise par un ré-enregistrement
// non contrôlé.
func SetAgentPersistent(database *sql.DB, driver, agentId string, persistent bool) error {
	val := 0
	if persistent {
		val = 1
	}
	_, err := database.Exec(`UPDATE agents SET persistent = ? WHERE agent_id = ?`, val, agentId)
	return err
}

// SetAgentState change l'état d'un agent. Utilisé pour poser
// 'pending_reregistration' (mode approval de #87) puis le lever après
// approbation par un admin.
func SetAgentState(database *sql.DB, driver, agentId, state string) error {
	_, err := database.Exec(`UPDATE agents SET state = ? WHERE agent_id = ?`, state, agentId)
	return err
}

// OpenAgentReregistrationWindow ouvre une fenêtre temporelle de
// ré-enregistrement pour un nœud persistant (#87, mode window). `until` est la
// borne limite (UTC) avant laquelle un ré-enregistrement est accepté.
func OpenAgentReregistrationWindow(database *sql.DB, driver, agentId string, until time.Time) error {
	var val interface{}
	if driver == "mssql" || driver == "sqlserver" {
		val = until.UTC()
	} else {
		val = until.UTC().Format("2006-01-02 15:04:05")
	}
	_, err := database.Exec(`UPDATE agents SET reregistration_window_until = ? WHERE agent_id = ?`, val, agentId)
	return err
}

// CloseAgentReregistrationWindow ferme (efface) la fenêtre temporelle de
// ré-enregistrement d'un nœud persistant. Utilisé après approbation manuelle ou
// quand la fenêtre ne doit plus autoriser de ré-enregistrement.
func CloseAgentReregistrationWindow(database *sql.DB, driver, agentId string) error {
	_, err := database.Exec(`UPDATE agents SET reregistration_window_until = NULL WHERE agent_id = ?`, agentId)
	return err
}
