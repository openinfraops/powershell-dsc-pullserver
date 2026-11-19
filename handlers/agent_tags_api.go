package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/global"
)

// GET /api/v1/agents/{id}/tags : liste tous les tags clé/valeur d'un agent
func AgentTagsListHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	database, err := db.OpenDB(&global.AppConfig.Database)
	       if err != nil {
			   logs.WriteLogFile("AgentTagsListHandler: DB open error: " + err.Error())
		       http.Error(w, "DB open error", http.StatusInternalServerError)
		       return
	       }
	       defer database.Close()
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
	   database, err := db.OpenDB(&global.AppConfig.Database)
	   if err != nil {
		   logs.WriteLogFile("AgentTagsSetHandler: DB open error: " + err.Error())
		   http.Error(w, "DB open error", http.StatusInternalServerError)
		   return
	   }
	   defer database.Close()
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
	if err := db.SetAgentTag(database, global.AppConfig.Database.Driver, agentId, req.Key, req.Value); err != nil {
		logs.WriteLogFile("AgentTagsSetHandler: DB update error: " + err.Error())
		http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Un changement de tag rend la configuration du nœud obsolète.
	_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
	   w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/agents/{id}/tags : supprime une valeur précise d'un tag (key/value) ou tous les tags
func AgentTagsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	   agentId := r.PathValue("id")
	   database, err := db.OpenDB(&global.AppConfig.Database)
	   if err != nil {
		   logs.WriteLogFile("AgentTagsDeleteHandler: DB open error: " + err.Error())
		   http.Error(w, "DB open error", http.StatusInternalServerError)
		   return
	   }
	   defer database.Close()
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
		   if err := db.DeleteAgentTag(database, agentId, key, value); err != nil {
			   logs.WriteLogFile("AgentTagsDeleteHandler: DB delete error: " + err.Error())
			   http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
			   return
		   }
		   _, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
	   } else {
		   if err := db.DeleteAllAgentTags(database, agentId); err != nil {
			   logs.WriteLogFile("AgentTagsDeleteHandler: DB delete-all error: " + err.Error())
			   http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
			   return
		   }
		   _, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, agentId)
	   }
	   w.WriteHeader(http.StatusNoContent)
}