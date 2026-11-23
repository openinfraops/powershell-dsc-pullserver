package handlers

import (
	"log"
	"net/http"
	"strings"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/utils"
)

// extractJobId extrait le JobId d'un segment de type Reports(JobId='...') (ou
// Reports(JobId="...")). Retourne "" si le segment n'est pas au format attendu.
func extractJobId(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, delim := range []string{"(JobId='", `(JobId="`} {
		if start := strings.Index(raw, delim); start != -1 {
			closer := "'"
			if strings.HasPrefix(delim, `(JobId="`) {
				closer = `"`
			}
			end := strings.LastIndex(raw, closer+")")
			if end != -1 && end > start+len(delim) {
				return raw[start+len(delim) : end]
			}
		}
	}
	// Fallback : JobId nu (GUID sans wrapper).
	if !strings.HasPrefix(raw, "Reports(") {
		return strings.Trim(raw, "'\"")
	}
	return ""
}

// GetReportsHandler gère GET /PSDSCPullServer.svc/Nodes(AgentId='...')/Reports(JobId='...')
// (§3.11 du MS-DSCPM). Renvoie le rapport complet (raw_json) tel qu'il a été
// envoyé par le client via SendReport — c'est le « ReportContent » (BLOB) de la
// spec — avec Content-Type application/json et ProtocolVersion 2.0.
func GetReportsHandler(w http.ResponseWriter, r *http.Request) {
	nodeRaw := r.PathValue("node")
	reportsRaw := r.PathValue("reports")
	agentId := utils.ExtractAgentId(nodeRaw)
	jobId := extractJobId(reportsRaw)
	if agentId == "" || jobId == "" {
		http.Error(w, "Bad request: missing AgentId or JobId", http.StatusBadRequest)
		return
	}
	log.Printf("[GETREPORTS] AgentId=%s JobId=%s (raw node=%q reports=%q)", agentId, jobId, nodeRaw, reportsRaw)

	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Liaison identité : le certificat client présenté doit correspondre au nœud
	// dont on lit le rapport.
	if ok, cause := verifyNodeCertBinding(r, database, agentId); !ok {
		log.Printf("[GETREPORTS] Certificate-to-node binding refused for agentId=%q: %s", agentId, cause)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var query string
	switch global.AppConfig.Database.Driver {
	case "sqlite":
		query = `SELECT raw_json FROM reports WHERE agent_id = ? AND job_id = ? ORDER BY created_at DESC LIMIT 1`
	case "mssql", "sqlserver":
		query = `SELECT TOP 1 raw_json FROM reports WHERE agent_id = ? AND job_id = ? ORDER BY created_at DESC`
	default:
		query = `SELECT raw_json FROM reports WHERE agent_id = ? AND job_id = ? ORDER BY created_at DESC LIMIT 1`
	}
	var raw string
	if err := database.QueryRow(query, agentId, jobId).Scan(&raw); err != nil {
		log.Printf("[GETREPORTS] Report not found for agentId=%s jobId=%s: %v", agentId, jobId, err)
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ProtocolVersion", "2.0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(raw))
}
