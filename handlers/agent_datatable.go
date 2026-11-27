package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// agentDTRow est la ligne renvoyée à DataTable pour la page Nodes (sans les
// champs lourds/inutiles pour une liste).
type agentDTRow struct {
	AgentID               string  `json:"agent_id"`
	NodeName              string  `json:"node_name"`
	LCMVersion            *string `json:"lcm_version,omitempty"`
	RegistrationType      *string `json:"registration_type,omitempty"`
	State                 *string `json:"state,omitempty"`
	CertificateThumbprint *string `json:"certificate_thumbprint,omitempty"`
	CertificateNotAfter   *string `json:"certificate_notafter,omitempty"`
	LastCommunication     string  `json:"last_communication"`
	HasErrorLastReport    bool    `json:"has_error_last_report"`
	NeedsRecompile        bool    `json:"needs_recompile"`
	RebootPending         bool    `json:"reboot_pending"`
	RebootPendingSince    *string `json:"reboot_pending_since,omitempty"`
	RebootRequestedBy     *string `json:"reboot_requested_by,omitempty"`
	RebootLoop            bool    `json:"reboot_loop"`
	RebootRequestCount    int     `json:"reboot_request_count"`
	Persistent            bool    `json:"persistent"`
	DeletedAt             *string `json:"deleted_at,omitempty"`
	DeletedBy             *string `json:"deleted_by,omitempty"`
}

// agentDTResponse est la réponse JSON attendue par DataTable en mode serverSide.
type agentDTResponse struct {
	Draw            int          `json:"draw"`
	RecordsTotal    int          `json:"recordsTotal"`
	RecordsFiltered int          `json:"recordsFiltered"`
	Data            []agentDTRow `json:"data"`
}

// staleCutoff retourne la date limite de "nœud muet" : un nœud dont la dernière
// communication est antérieure à `days` jours est considéré muet.
func staleCutoff(days int) time.Time {
	if days <= 0 {
		days = 7
	}
	return time.Now().UTC().AddDate(0, 0, -days)
}

// certNotAfterExpr retourne l'expression SQL normalisée de certificate_notafter
// (texte "YYYY-MM-DD HH:MM:SS" ou RFC3339 -> type date comparables). Driver-aware.
func certNotAfterExpr(driver string) string {
	if isMSSQLDriver(driver) {
		return "TRY_CONVERT(datetime, REPLACE(REPLACE(certificate_notafter,'T',' '),'Z',''))"
	}
	return "julianday(REPLACE(REPLACE(certificate_notafter,'T',' '),'Z',''))"
}

// certAtRiskClause retourne la clause WHERE (et son argument) pour filtrer les
// nœuds dont le certificat est expiré ou expire sous `days` jours (par défaut
// 30). Les nœuds sans certificat sont inclus.
func certAtRiskClause(driver string, days int) (string, interface{}) {
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, days)
	expr := certNotAfterExpr(driver)
	if isMSSQLDriver(driver) {
		return " AND (certificate_notafter IS NULL OR certificate_notafter = '' OR " + expr + " < ?)", cutoff
	}
	return " AND (certificate_notafter IS NULL OR certificate_notafter = '' OR " + expr + " < julianday(?))", cutoff.Format("2006-01-02 15:04:05")
}

// ListAgentsDataTableHandler implémente le protocole serverSide de DataTables
// pour la page Nodes : pagination, tri de colonnes, recherche globale et les
// mêmes filtres que GetAgentsHandler (corbeille, node_name, has_error_last_report,
// tag, stale). Il est branché derrière /api/v1/agents dès que draw est présent.
func ListAgentsDataTableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	database := db.Shared()

	draw, _ := strconv.Atoi(r.URL.Query().Get("draw"))
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	length, _ := strconv.Atoi(r.URL.Query().Get("length"))
	if length <= 0 || length > 5000 {
		length = 25
	}
	if start < 0 {
		start = 0
	}

	search := strings.TrimSpace(r.URL.Query().Get("search[value]"))

	// Tri : l'index de colonne DataTable (user) est validé et mappé vers une
	// colonne SQL fixe, sans jamais concaténer la valeur utilisateur dans la
	// requête (ORDER BY). Le sens de tri est limité à 'asc'/'desc'.
	orderColIdx, _ := strconv.Atoi(r.URL.Query().Get("order[0][column]"))
	orderCol := "node_name"
	switch orderColIdx {
	case 2:
		orderCol = "last_communication"
	case 3:
		orderCol = "certificate_thumbprint"
	case 4:
		orderCol = "state"
	case 5:
		orderCol = "certificate_notafter"
	case 6:
		orderCol = "agent_id"
	}
	orderDir := "asc"
	if r.URL.Query().Get("order[0][dir]") == "desc" {
		orderDir = "desc"
	}

	isMSSQL := isMSSQLDriver(global.AppConfig.Database.Driver)

	where := " WHERE 1=1"
	args := []interface{}{}

	// Filtre corbeille : ?deleted=true liste les nœuds supprimés, sinon actifs.
	if r.URL.Query().Get("deleted") == "true" {
		where += " AND deleted_at IS NOT NULL"
	} else {
		where += " AND deleted_at IS NULL"
	}

	if nodeName := strings.TrimSpace(r.URL.Query().Get("node_name")); nodeName != "" {
		where += " AND node_name = ?"
		args = append(args, nodeName)
	}

	switch r.URL.Query().Get("has_error_last_report") {
	case "true":
		where += " AND has_error_last_report = 1"
	case "false":
		where += " AND has_error_last_report = 0"
	}

	// Filtres de redémarrage : ?reboot_pending=true|false, ?reboot_loop=true.
	switch r.URL.Query().Get("reboot_pending") {
	case "true":
		where += " AND reboot_pending = 1"
	case "false":
		where += " AND reboot_pending = 0"
	}
	if r.URL.Query().Get("reboot_loop") == "true" {
		where += " AND reboot_loop = 1"
	}

	// Filtre nœuds persistants : ?persistent=true|false.
	switch r.URL.Query().Get("persistent") {
	case "true":
		where += " AND persistent = 1"
	case "false":
		where += " AND persistent = 0"
	}

	for _, rawTag := range r.URL.Query()["tag"] {
		tagKey, tagValue, found := strings.Cut(rawTag, ":")
		if !found || tagKey == "" || tagValue == "" {
			http.Error(w, "Invalid tag filter, expected tag=key:value", http.StatusBadRequest)
			return
		}
		where += " AND EXISTS (SELECT 1 FROM agent_tags at WHERE at.agent_id = agents.agent_id AND at.tag_key = ? AND at.tag_value = ?)"
		args = append(args, tagKey, tagValue)
	}

	// Filtre "nœuds muets" : dernière communication trop ancienne.
	if r.URL.Query().Get("stale") == "true" {
		staleDays, _ := strconv.Atoi(r.URL.Query().Get("stale_days"))
		cutoff := staleCutoff(staleDays)
		if isMSSQL {
			where += " AND (last_communication IS NULL OR last_communication < ?)"
			args = append(args, cutoff)
		} else {
			where += " AND COALESCE(julianday(COALESCE(last_communication, '')), 0) < julianday(?)"
			args = append(args, cutoff.Format("2006-01-02 15:04:05"))
		}
	}

	// Filtre "certificats à risque" : expiré ou expirant sous X jours.
	if r.URL.Query().Get("cert_at_risk") == "true" {
		days, _ := strconv.Atoi(r.URL.Query().Get("cert_expiring_days"))
		clause, cutoff := certAtRiskClause(global.AppConfig.Database.Driver, days)
		where += clause
		args = append(args, cutoff)
	}

	if search != "" {
		pattern := "%" + search + "%"
		where += " AND (node_name LIKE ? OR agent_id LIKE ? OR COALESCE(certificate_thumbprint, '') LIKE ? OR COALESCE(state, '') LIKE ?)"
		args = append(args, pattern, pattern, pattern, pattern)
	}

	countBase := "SELECT COUNT(*) FROM agents" + where

	var recordsTotal int
	if r.URL.Query().Get("deleted") == "true" {
		if err := database.QueryRow("SELECT COUNT(*) FROM agents WHERE deleted_at IS NOT NULL").Scan(&recordsTotal); err != nil {
			http.Error(w, "DB count error", http.StatusInternalServerError)
			return
		}
	} else {
		if err := database.QueryRow("SELECT COUNT(*) FROM agents WHERE deleted_at IS NULL").Scan(&recordsTotal); err != nil {
			http.Error(w, "DB count error", http.StatusInternalServerError)
			return
		}
	}

	var recordsFiltered int
	// #nosec G701 -- query is composed from fixed fragments plus parameterized
	// `?` placeholders (all user values travel in args); ORDER BY is whitelisted.
	if err := database.QueryRow(countBase, args...).Scan(&recordsFiltered); err != nil {
		http.Error(w, "DB count error", http.StatusInternalServerError)
		return
	}

	query := "SELECT agent_id, node_name, lcm_version, registration_type, state, certificate_thumbprint, certificate_notafter, last_communication, has_error_last_report, needs_recompile, reboot_pending, reboot_pending_since, reboot_requested_by, reboot_loop, reboot_request_count, persistent, deleted_at, deleted_by FROM agents" + where
	query += " ORDER BY " + orderCol + " " + orderDir

	if isMSSQL {
		query += " OFFSET ? ROWS FETCH NEXT ? ROWS ONLY"
		args = append(args, start, length)
	} else {
		query += " LIMIT ? OFFSET ?"
		args = append(args, length, start)
	}

	// #nosec G701 -- query is composed from fixed fragments plus parameterized
	// `?` placeholders (all user values travel in args); ORDER BY is whitelisted.
	rows, err := database.Query(query, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	data := make([]agentDTRow, 0)
	for rows.Next() {
		var row agentDTRow
		var lcmVersion, registrationType, state, certThumb, certNotAfter, deletedAt, deletedBy *string
		var rebootPendingSince, rebootRequestedBy *string
		var hasErr, needsRecompile, rebootPending, rebootLoop, persistent bool
		var rebootRequestCount int
		if err := rows.Scan(&row.AgentID, &row.NodeName, &lcmVersion, &registrationType, &state, &certThumb, &certNotAfter, &row.LastCommunication, &hasErr, &needsRecompile, &rebootPending, &rebootPendingSince, &rebootRequestedBy, &rebootLoop, &rebootRequestCount, &persistent, &deletedAt, &deletedBy); err != nil {
			http.Error(w, "DB scan error", http.StatusInternalServerError)
			return
		}
		row.LCMVersion = lcmVersion
		row.RegistrationType = registrationType
		row.State = state
		row.CertificateThumbprint = certThumb
		row.CertificateNotAfter = certNotAfter
		row.HasErrorLastReport = hasErr
		row.NeedsRecompile = needsRecompile
		row.RebootPending = rebootPending
		row.RebootPendingSince = rebootPendingSince
		row.RebootRequestedBy = rebootRequestedBy
		row.RebootLoop = rebootLoop
		row.RebootRequestCount = rebootRequestCount
		row.Persistent = persistent
		row.DeletedAt = deletedAt
		row.DeletedBy = deletedBy
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentDTResponse{
		Draw:            draw,
		RecordsTotal:    recordsTotal,
		RecordsFiltered: recordsFiltered,
		Data:            data,
	})
}
