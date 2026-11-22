package handlers

import (
	"encoding/csv"
	"log"
	"net/http"
	"strconv"
	"strings"

	"go-dsc-pull/internal/db"
)

// ExportAgentReportsCSVHandler exports the DSC reports of a single agent as a
// CSV file.
//
//	GET /api/v1/agents/{id}/reports/export
//
// It accepts the same optional filters as AgentReportsListHandler
// (operationtype, mofapplied) and returns richer columns than the summary list
// (job_id, created_at, status, operation_type, mof_applied, start_time,
// end_time). A UTF-8 BOM is prepended so Excel opens the file correctly.
func ExportAgentReportsCSVHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	agentID := r.PathValue("id")
	if agentID == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}

	database := db.Shared()

	query := `SELECT job_id, created_at, status, operation_type, mof_applied, start_time, end_time FROM reports WHERE agent_id = ?`
	params := []interface{}{agentID}

	if opType := strings.TrimSpace(r.URL.Query().Get("operationtype")); opType != "" {
		query += " AND LOWER(operation_type) = ?"
		params = append(params, strings.ToLower(opType))
	}
	if mofApplied := r.URL.Query().Get("mofapplied"); mofApplied != "" {
		query += " AND mof_applied = ?"
		params = append(params, mofApplied)
	}
	query += " ORDER BY created_at DESC"

	rows, err := database.Query(query, params...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"agent_reports_export.csv\"")
	// BOM UTF-8 pour Excel.
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"job_id",
		"created_at",
		"status",
		"operation_type",
		"mof_applied",
		"start_time",
		"end_time",
	})

	for rows.Next() {
		var jobID, createdAt, status, operationType string
		var mofApplied int
		var startTime, endTime *string
		if err := rows.Scan(&jobID, &createdAt, &status, &operationType, &mofApplied, &startTime, &endTime); err != nil {
			log.Printf("[CSV][REPORTS] row skipped, scan error: %v", err)
			continue
		}
		_ = cw.Write([]string{
			jobID,
			createdAt,
			status,
			operationType,
			strconv.Itoa(mofApplied),
			deref(startTime),
			deref(endTime),
		})
	}
}
