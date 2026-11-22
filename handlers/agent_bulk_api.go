package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// bulkAgentReq est le corps commun des opérations en masse sur les nœuds.
type bulkAgentReq struct {
	AgentIDs []string `json:"agent_ids"`
	Key      string   `json:"key"`
	Value    string   `json:"value"`
}

// bulkAgentResult est la réponse commune des opérations en masse.
type bulkAgentResult struct {
	Processed int      `json:"processed"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
}

func decodeBulkAgentReq(r *http.Request) (*bulkAgentReq, error) {
	var req bulkAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.New("invalid JSON body")
	}
	if len(req.AgentIDs) == 0 {
		return nil, errors.New("agent_ids is required")
	}
	// Déduplique et ignore les ids vides.
	seen := map[string]bool{}
	ids := make([]string, 0, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("agent_ids is empty")
	}
	req.AgentIDs = ids
	return &req, nil
}

func writeBulkResult(w http.ResponseWriter, processed, failed int, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bulkAgentResult{
		Processed: processed,
		Failed:    failed,
		Errors:    errs,
	})
}

// BulkAgentsRecompileHandler déclenche la recompilation de la configuration de
// plusieurs nœuds. Réservé aux admins.
//
//	POST /api/v1/agents/bulk/recompile  {"agent_ids": ["..."]}
func BulkAgentsRecompileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBulkAgentReq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	processed, failed := 0, 0
	errs := []string{}
	for _, id := range req.AgentIDs {
		var nodeName string
		if err := database.QueryRow(`SELECT node_name FROM agents WHERE agent_id = ?`, id).Scan(&nodeName); err != nil || strings.TrimSpace(nodeName) == "" {
			failed++
			errs = append(errs, id+": agent not found")
			continue
		}
		triggerNodeRecompileAsync(id, nodeName)
		processed++
	}
	writeBulkResult(w, processed, failed, errs)
}

// BulkAgentsTrashHandler met plusieurs nœuds en corbeille (soft delete).
//
//	POST /api/v1/agents/bulk/trash  {"agent_ids": ["..."]}
func BulkAgentsTrashHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBulkAgentReq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	deletedBy := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok && sub != "" {
			deletedBy = sub
		}
	}
	driver := global.AppConfig.Database.Driver

	processed, failed := 0, 0
	errs := []string{}
	for _, id := range req.AgentIDs {
		if err := db.SoftDeleteAgent(database, driver, id, deletedBy); err != nil {
			failed++
			errs = append(errs, id+": "+err.Error())
			continue
		}
		_ = db.InsertAudit(database, driver, deletedBy, "trash", "agent", "Moved agent to trash: "+id, "")
		processed++
	}
	writeBulkResult(w, processed, failed, errs)
}

// BulkAgentsRestoreHandler sort plusieurs nœuds de la corbeille.
//
//	POST /api/v1/agents/bulk/restore  {"agent_ids": ["..."]}
func BulkAgentsRestoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBulkAgentReq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	deletedBy := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok && sub != "" {
			deletedBy = sub
		}
	}
	driver := global.AppConfig.Database.Driver

	processed, failed := 0, 0
	errs := []string{}
	for _, id := range req.AgentIDs {
		if err := db.RestoreAgent(database, driver, id); err != nil {
			failed++
			errs = append(errs, id+": "+err.Error())
			continue
		}
		// Au retour de corbeille, la configuration doit être recalculée.
		_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, id)
		var nodeName string
		_ = database.QueryRow(`SELECT node_name FROM agents WHERE agent_id = ?`, id).Scan(&nodeName)
		triggerNodeRecompileAsync(id, nodeName)
		_ = db.InsertAudit(database, driver, deletedBy, "restore", "agent", "Restored agent from trash: "+id, "")
		processed++
	}
	writeBulkResult(w, processed, failed, errs)
}

// BulkAgentsTagsHandler applique un tag clé/valeur à plusieurs nœuds.
//
//	PUT /api/v1/agents/bulk/tags  {"agent_ids": ["..."], "key": "env", "value": "prod"}
func BulkAgentsTagsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeBulkAgentReq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Value) == "" {
		http.Error(w, "key and value are required", http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	driver := global.AppConfig.Database.Driver
	processed, failed := 0, 0
	errs := []string{}
	for _, id := range req.AgentIDs {
		if err := db.SetAgentTag(database, driver, id, req.Key, req.Value); err != nil {
			failed++
			errs = append(errs, id+": "+err.Error())
			continue
		}
		// Un changement de tag rend la configuration obsolète.
		_, _ = database.Exec(`UPDATE agents SET needs_recompile = 1 WHERE agent_id = ?`, id)
		processed++
	}
	writeBulkResult(w, processed, failed, errs)
}
