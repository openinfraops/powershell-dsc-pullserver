package handlers

import (
	"encoding/json"
	"net/http"
	"go-dsc-pull/internal/db"
)

// AgentConfigsAPIHandler retourne la liste des configurations pour un agent donné
func AgentConfigsAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()

	rows, err := database.Query(`SELECT configuration_name FROM agent_configurations WHERE agent_id = ?`, agentId)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	configs := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			configs = append(configs, name)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(configs)
}
