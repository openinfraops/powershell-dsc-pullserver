package handlers

import (
	"database/sql"
	"encoding/json"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"net/http"
	"strings"
)

// AgentReportsListHandler retourne la liste des rapports d'un agent
func AgentReportsListHandler(w http.ResponseWriter, r *http.Request) {
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()

	// Récupère le filtre operationType et mofApplied si présents
	opType := r.URL.Query().Get("operationtype")
	mofApplied := r.URL.Query().Get("mofapplied")
	var rows *sql.Rows
	var debugMsg string
	query := `SELECT id, job_id, created_at, status, reboot_requested FROM reports WHERE agent_id = ?`
	params := []interface{}{agentId}
	if opType != "" {
		opTypeLower := strings.ToLower(opType)
		query += " AND LOWER(operation_type) = ?"
		params = append(params, opTypeLower)
		debugMsg = "Filtre operationType: '" + opTypeLower + "'"
	} else {
		debugMsg = "Pas de filtre operationType"
	}
	if mofApplied != "" {
		query += " AND mof_applied = ?"
		params = append(params, mofApplied)
		debugMsg += ", mof_applied=" + mofApplied
	}
	query += " ORDER BY created_at DESC"
	rows, err := database.Query(query, params...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	reports := []schema.ReportSummary{}
	count := 0
	for rows.Next() {
		var rep schema.ReportSummary
		var rebootRequested string
		if err := rows.Scan(&rep.ID, &rep.JobId, &rep.CreatedAt, &rep.Status, &rebootRequested); err == nil {
			rep.RebootRequested = schema.ParseBoolFlexible(rebootRequested)
			reports = append(reports, rep)
			count++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Debug-OperationType", debugMsg+" | Results: "+string(rune(count)))
	_ = json.NewEncoder(w).Encode(reports)
}
