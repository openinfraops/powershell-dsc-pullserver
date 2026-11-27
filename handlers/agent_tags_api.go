package handlers

import (
	"encoding/json"
	"fmt"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"io"
	"net/http"
)

// GET /api/v1/agents/{id}/tags : liste tous les tags clé/valeur d'un agent
func AgentTagsListHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	database := db.Shared()
	tags, err := db.GetAgentTags(database, agentId)
	if err != nil {
		logs.WriteLogFile("AgentTagsListHandler: DB query error: " + err.Error())
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tags)
}

// PUT /api/v1/agents/{id}/tags : ajoute une valeur à un tag clé pour un agent
func AgentTagsSetHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" || req.Value == "" {
		if err != nil {
			logs.WriteLogFile("AgentTagsSetHandler: Invalid key/value: " + err.Error())
		} else {
			logs.WriteLogFile("AgentTagsSetHandler: Invalid key/value: missing key or value")
		}
		http.Error(w, "Invalid key/value", http.StatusBadRequest)
		return
	}
	// Capture the previous values of this tag key so the audit records the
	// before/after state, with emphasis on the "environment" tag.
	prev := tagValuesForAudit(database, agentId, req.Key)
	if err := db.SetAgentTag(database, global.AppConfig.Database.Driver, agentId, req.Key, req.Value); err != nil {
		logs.WriteLogFile("AgentTagsSetHandler: DB update error: " + err.Error())
		http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Un changement de tag rend la configuration du nœud obsolète.
	_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
	_ = db.InsertAudit(database, global.AppConfig.Database.Driver, currentUserEmail(r, database),
		"tag.set", "agent",
		fmt.Sprintf("Set tag %s=%s on agent %s (previous: %s)", req.Key, req.Value, agentId, prev),
		auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies))
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/agents/{id}/tags : supprime une valeur précise d'un tag (key/value) ou tous les tags
func AgentTagsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	key := r.URL.Query().Get("key")
	value := r.URL.Query().Get("value")

	if key == "" || value == "" {
		var req struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			key = req.Key
			value = req.Value
		} else if err != io.EOF {
			logs.WriteLogFile("AgentTagsDeleteHandler: Invalid payload: " + err.Error())
			http.Error(w, "Invalid key/value", http.StatusBadRequest)
			return
		}
	}

	if key != "" || value != "" {
		if key == "" || value == "" {
			logs.WriteLogFile("AgentTagsDeleteHandler: Invalid key/value: missing key or value")
			http.Error(w, "Invalid key/value", http.StatusBadRequest)
			return
		}
		prev := tagValuesForAudit(database, agentId, key)
		if err := db.DeleteAgentTag(database, agentId, key, value); err != nil {
			logs.WriteLogFile("AgentTagsDeleteHandler: DB delete error: " + err.Error())
			http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
		_ = db.InsertAudit(database, global.AppConfig.Database.Driver, currentUserEmail(r, database),
			"tag.delete", "agent",
			fmt.Sprintf("Delete tag %s=%s on agent %s (previous: %s)", key, value, agentId, prev),
			auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies))
	} else {
		prev := allTagsForAudit(database, agentId)
		if err := db.DeleteAllAgentTags(database, agentId); err != nil {
			logs.WriteLogFile("AgentTagsDeleteHandler: DB delete-all error: " + err.Error())
			http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
		_ = db.InsertAudit(database, global.AppConfig.Database.Driver, currentUserEmail(r, database),
			"tag.delete", "agent",
			fmt.Sprintf("Delete all tags on agent %s (previous: %s)", agentId, prev),
			auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies))
	}
	w.WriteHeader(http.StatusNoContent)
}
