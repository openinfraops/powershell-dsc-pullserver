package handlers

import (
	"encoding/csv"
	"log"
	"net/http"
	"strconv"
	"strings"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// ExportAgentsCSVHandler exports the agents (nodes) list as a CSV file.
//
//	GET /api/v1/agents/export
//
// It mirrors the filters of GetAgentsHandler (deleted=true for the trash,
// node_name, has_error_last_report, tag=key:value) so the exported dataset
// matches what the user sees in the Nodes page. A UTF-8 BOM is prepended so
// Excel opens the file with the correct encoding.
func ExportAgentsCSVHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	database := db.Shared()

	q := `SELECT node_name, agent_id, lcm_version, registration_type, state, registered_at, last_communication, certificate_notafter, needs_recompile, deleted_at, deleted_by FROM agents WHERE 1=1`
	args := []interface{}{}

	if nodeName := strings.TrimSpace(r.URL.Query().Get("node_name")); nodeName != "" {
		q += " AND node_name = ?"
		args = append(args, nodeName)
	}

	// Filtre corbeille : ?deleted=true liste les nœuds supprimés, sinon actifs.
	if r.URL.Query().Get("deleted") == "true" {
		q += " AND deleted_at IS NOT NULL"
	} else {
		q += " AND deleted_at IS NULL"
	}

	switch r.URL.Query().Get("has_error_last_report") {
	case "true":
		q += " AND has_error_last_report = 1"
	case "false":
		q += " AND has_error_last_report = 0"
	}

	// Filtre "nœuds muets" : dernière communication trop ancienne.
	if r.URL.Query().Get("stale") == "true" {
		staleDays, _ := strconv.Atoi(r.URL.Query().Get("stale_days"))
		cutoff := staleCutoff(staleDays)
		if isMSSQLDriver(global.AppConfig.Database.Driver) {
			q += " AND (last_communication IS NULL OR last_communication < ?)"
			args = append(args, cutoff)
		} else {
			q += " AND COALESCE(julianday(COALESCE(last_communication, '')), 0) < julianday(?)"
			args = append(args, cutoff.Format("2006-01-02 15:04:05"))
		}
	}

	// Filtre "certificats à risque" : expiré ou expirant sous X jours.
	if r.URL.Query().Get("cert_at_risk") == "true" {
		days, _ := strconv.Atoi(r.URL.Query().Get("cert_expiring_days"))
		clause, cutoff := certAtRiskClause(global.AppConfig.Database.Driver, days)
		q += clause
		args = append(args, cutoff)
	}

	for _, rawTag := range r.URL.Query()["tag"] {
		tagKey, tagValue, found := strings.Cut(rawTag, ":")
		if !found || tagKey == "" || tagValue == "" {
			http.Error(w, "Invalid tag filter, expected tag=key:value", http.StatusBadRequest)
			return
		}
		q += " AND EXISTS (SELECT 1 FROM agent_tags at WHERE at.agent_id = agents.agent_id AND at.tag_key = ? AND at.tag_value = ?)"
		args = append(args, tagKey, tagValue)
	}

	q += " ORDER BY node_name ASC"

	// #nosec G701 -- query is composed from fixed fragments plus parameterized
	// `?` placeholders (all user values travel in args); ORDER BY is a constant.
	rows, err := database.Query(q, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"agents_export.csv\"")
	// BOM UTF-8 pour Excel.
	w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"node_name",
		"agent_id",
		"lcm_version",
		"registration_type",
		"state",
		"registered_at",
		"last_communication",
		"certificate_notafter",
		"needs_recompile",
		"deleted_at",
		"deleted_by",
	})

	for rows.Next() {
		var nodeName, agentID string
		var lcmVersion, registrationType, state, registeredAt, lastComm, certNotAfter, deletedAt, deletedBy *string
		var needsRecompile bool
		if err := rows.Scan(&nodeName, &agentID, &lcmVersion, &registrationType, &state, &registeredAt, &lastComm, &certNotAfter, &needsRecompile, &deletedAt, &deletedBy); err != nil {
			log.Printf("[CSV][AGENTS] row skipped, scan error: %v", err)
			continue
		}
		_ = cw.Write([]string{
			nodeName,
			agentID,
			deref(lcmVersion),
			deref(registrationType),
			deref(state),
			deref(registeredAt),
			deref(lastComm),
			deref(certNotAfter),
			strconv.FormatBool(needsRecompile),
			deref(deletedAt),
			deref(deletedBy),
		})
	}
}

// deref returns the pointed value, or an empty string when the pointer is nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
