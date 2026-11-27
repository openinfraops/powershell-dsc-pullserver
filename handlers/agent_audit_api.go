package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// agentAuditEntry est la représentation JSON d'une entrée d'audit renvoyée par
// l'historique de modifications d'un nœud.
type agentAuditEntry struct {
	ID        int64  `json:"id"`
	UserEmail string `json:"user_email"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Details   string `json:"details"`
	IPAddress string `json:"ip_address"`
	CreatedAt string `json:"created_at"`
}

// maxAgentAuditEntries borne le nombre de lignes renvoyées par l'historique
// d'un nœud (les plus récentes).
const maxAgentAuditEntries = 100

// AgentAuditListHandler renvoie l'historique des modifications d'un agent
// (GET /api/v1/agents/{id}/audit), de la plus récente à la plus ancienne.
//
// Réservé aux admins (l'audit est une information sensible) : la route est
// enveloppée par adminOnly et on vérifie aussi auth.IsAdmin ici. Les entrées
// sont identifiées via l'id du nœud, qui figure dans le message d'audit des
// actions qui le concernent (preenroll, tag.set, tag.delete, trash, restore…).
func AgentAuditListHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	if agentId == "" {
		http.Error(w, "Agent id required", http.StatusBadRequest)
		return
	}

	like := "%" + strings.ToLower(agentId) + "%"
	var query string
	var args []interface{}
	if global.AppConfig.Database.Driver == "mssql" || global.AppConfig.Database.Driver == "sqlserver" {
		query = `SELECT id, user_email, action, target, details, ip_address, created_at
			FROM audit WHERE LOWER(COALESCE(details, '')) LIKE ?
			ORDER BY created_at DESC OFFSET 0 ROWS FETCH NEXT ? ROWS ONLY`
		args = []interface{}{like, maxAgentAuditEntries}
	} else {
		query = `SELECT id, user_email, action, target, details, ip_address, created_at
			FROM audit WHERE LOWER(COALESCE(details, '')) LIKE ?
			ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{like, maxAgentAuditEntries}
	}

	rows, err := database.Query(query, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]agentAuditEntry, 0)
	for rows.Next() {
		var e agentAuditEntry
		if err := rows.Scan(&e.ID, &e.UserEmail, &e.Action, &e.Target, &e.Details, &e.IPAddress, &e.CreatedAt); err == nil {
			items = append(items, e)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}
