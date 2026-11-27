package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
)

// SetAgentPersistentHandler marque/démarque un nœud comme persistant (#87).
// PUT /api/v1/agents/{id}/persistent, corps JSON {"persistent": true|false}.
// Admin uniquement (filtré au niveau des routes).
func SetAgentPersistentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	var req struct {
		Persistent bool `json:"persistent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var exists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ? AND deleted_at IS NULL`, agentId).Scan(&exists); err != nil || exists == 0 {
		http.Error(w, "Agent non trouvé", http.StatusNotFound)
		return
	}

	driver := global.AppConfig.Database.Driver
	if err := db.SetAgentPersistent(database, driver, agentId, req.Persistent); err != nil {
		logs.WriteLogFile(fmt.Sprintf("ERROR [API][PERSISTENT] Error setting persistent on %s: %v", agentId, err))
		http.Error(w, "DB update error", http.StatusInternalServerError)
		return
	}

	// L'ouverture d'une fenêtre de ré-enregistrement n'a de sens que pour un
	// nœud persistant ; si on démarque le nœud, on neutralise la fenêtre.
	if !req.Persistent {
		_ = db.CloseAgentReregistrationWindow(database, driver, agentId)
	}

	actor := currentUserEmail(r, database)
	ip := auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies)
	_ = db.InsertAudit(database, driver, actor, "agent.persistent", "agent",
		fmt.Sprintf("Set persistent=%t for agent %s", req.Persistent, agentId), ip)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agent_id": agentId, "persistent": req.Persistent})
}

// ApproveReregistrationHandler lève l'état 'pending_reregistration' d'un nœud
// persistant après approbation admin (#87). La configuration redevient servie.
// POST /api/v1/agents/{id}/approve-reregistration. Admin uniquement.
func ApproveReregistrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}

	var pendingCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ? AND deleted_at IS NULL AND LOWER(COALESCE(state, '')) = 'pending_reregistration'`, agentId).Scan(&pendingCount); err != nil || pendingCount == 0 {
		http.Error(w, "Agent non trouvé ou pas en attente de ré-enregistrement", http.StatusNotFound)
		return
	}

	driver := global.AppConfig.Database.Driver
	// Repasse en état normal de traitement ; la configuration sera recalculée
	// et redevient servie.
	if err := db.SetAgentState(database, driver, agentId, "pending_apply"); err != nil {
		logs.WriteLogFile(fmt.Sprintf("ERROR [API][APPROVE-REREG] Error clearing pending_reregistration on %s: %v", agentId, err))
		http.Error(w, "DB update error", http.StatusInternalServerError)
		return
	}
	if _, err := database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId); err != nil {
		logs.WriteLogFile(fmt.Sprintf("ERROR [API][APPROVE-REREG] Error marking needs_recompile on %s: %v", agentId, err))
	}
	_ = db.CloseAgentReregistrationWindow(database, driver, agentId)

	var nodeName string
	_ = database.QueryRow(`SELECT node_name FROM agents WHERE agent_id = ?`, agentId).Scan(&nodeName)
	triggerNodeRecompileAsync(agentId, nodeName)

	actor := currentUserEmail(r, database)
	ip := auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies)
	_ = db.InsertAudit(database, driver, actor, "agent.approve_reregistration", "agent",
		fmt.Sprintf("Approved re-registration of persistent agent %s (node_name=%s)", agentId, nodeName), ip)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agent_id": agentId, "status": "approved"})
}

// SetReregistrationWindowHandler ouvre une fenêtre temporelle de
// ré-enregistrement pour un nœud persistant (#87, mode window).
// POST /api/v1/agents/{id}/reregistration-window, corps JSON
// {"hours": 4} (durée à partir de maintenant) ou {"until": "RFC3339"} (borne).
// Admin uniquement.
func SetReregistrationWindowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	var req struct {
		Hours int    `json:"hours"`
		Until string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var exists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ? AND deleted_at IS NULL`, agentId).Scan(&exists); err != nil || exists == 0 {
		http.Error(w, "Agent non trouvé", http.StatusNotFound)
		return
	}

	var until time.Time
	switch {
	case strings.TrimSpace(req.Until) != "":
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Until))
		if err != nil {
			http.Error(w, "until must be RFC3339", http.StatusBadRequest)
			return
		}
		until = t
	case req.Hours > 0:
		until = time.Now().UTC().Add(time.Duration(req.Hours) * time.Hour)
	default:
		http.Error(w, "hours (> 0) or until (RFC3339) required", http.StatusBadRequest)
		return
	}

	driver := global.AppConfig.Database.Driver
	if err := db.OpenAgentReregistrationWindow(database, driver, agentId, until); err != nil {
		logs.WriteLogFile(fmt.Sprintf("ERROR [API][REREG-WINDOW] Error opening window on %s: %v", agentId, err))
		http.Error(w, "DB update error", http.StatusInternalServerError)
		return
	}

	actor := currentUserEmail(r, database)
	ip := auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies)
	_ = db.InsertAudit(database, driver, actor, "agent.reregistration_window", "agent",
		fmt.Sprintf("Opened re-registration window for agent %s until %s", agentId, until.UTC().Format(time.RFC3339)), ip)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agent_id": agentId, "until": until.UTC().Format(time.RFC3339)})
}
