package handlers

import (
	"encoding/json"
	"net/http"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/global"
	"log"
)

// AgentReportsLatestHandler retourne le dernier rapport d'un agent
func AgentReportsLatestHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()

	var query string
	switch global.AppConfig.Database.Driver {
	case "sqlite":
		query = `SELECT raw_json FROM reports WHERE agent_id = ? ORDER BY created_at DESC LIMIT 1`
	case "mssql":
		query = `SELECT TOP 1 raw_json FROM reports WHERE agent_id = ? ORDER BY created_at DESC`
	default:
		query = `SELECT raw_json FROM reports WHERE agent_id = ? ORDER BY created_at DESC`
	}
	row := database.QueryRow(query, agentId)
	var raw string
	err := row.Scan(&raw)
	if err != nil {
		http.Error(w, "Aucun rapport trouvé", http.StatusNotFound)
		return
	}
	var report schema.DscReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		log.Printf("[API][REPORTS] Erreur parsing JSON: %v", err)
		http.Error(w, "Erreur parsing JSON", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}
