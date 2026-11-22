
package handlers

import (
	"encoding/json"
	"net/http"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// AgentConfigsAPIHandlerPostDelete gère POST (ajout) et DELETE (suppression) sur /api/v1/agents/{id}/configs
func AgentConfigsAPIHandlerPostDelete(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}

	database := db.Shared()

	switch r.Method {
	case http.MethodPost, http.MethodDelete:
		   // TODO: Replace with new admin check middleware if needed
		if r.Method == http.MethodPost {
			var req struct { ConfigurationName string `json:"configuration_name"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfigurationName == "" {
				http.Error(w, "Nom de configuration manquant ou invalide", http.StatusBadRequest)
				return
			}
			   driver := global.AppConfig.Database.Driver
			   if driver == "sqlite" {
				   _, err := database.Exec(`INSERT OR REPLACE INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, req.ConfigurationName)
				   if err != nil {
					   http.Error(w, "Erreur insertion config", http.StatusInternalServerError)
					   return
				   }
			   } else {
				   _, err := database.Exec(`IF NOT EXISTS (SELECT 1 FROM agent_configurations WHERE agent_id = ? AND configuration_name = ?) INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, req.ConfigurationName, agentId, req.ConfigurationName)
				   if err != nil {
					   http.Error(w, "Erreur insertion config", http.StatusInternalServerError)
					   return
				   }
			   }
			   w.WriteHeader(http.StatusCreated)
		} else {
			var req struct { ConfigurationName string `json:"configuration_name"` }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfigurationName == "" {
				http.Error(w, "Nom de configuration manquant ou invalide", http.StatusBadRequest)
				return
			}
			_, err := database.Exec(`DELETE FROM agent_configurations WHERE agent_id = ? AND configuration_name = ?`, agentId, req.ConfigurationName)
			if err != nil {
				http.Error(w, "Erreur suppression config", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
	}
}
