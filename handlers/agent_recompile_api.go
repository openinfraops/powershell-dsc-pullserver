package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
)

// AgentRecompileHandler déclenche la recompilation de la configuration d'un
// agent. Réservé aux admins. La compilation s'exécute en arrière-plan ; le flag
// needs_recompile de l'agent est remis à 0 quand elle aboutit.
func AgentRecompileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	var nodeName string
	if err := database.QueryRow(`SELECT node_name FROM agents WHERE agent_id = ?`, agentId).Scan(&nodeName); err != nil || strings.TrimSpace(nodeName) == "" {
		http.Error(w, "Agent non trouvé", http.StatusNotFound)
		return
	}
	triggerNodeRecompileAsync(agentId, nodeName)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "recompile_triggered", "agent_id": agentId})
}
