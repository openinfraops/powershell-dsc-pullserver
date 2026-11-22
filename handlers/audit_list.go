package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

func splitCSVValues(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parseDateParam(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func dateFilterArg(driverName string, t time.Time) interface{} {
	if driverName == "mssql" || driverName == "sqlserver" {
		return t.UTC()
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func lowerExpr(driverName, column string) string {
	if driverName == "mssql" || driverName == "sqlserver" {
		return "LOWER(ISNULL(" + column + ", ''))"
	}
	return "LOWER(COALESCE(" + column + ", ''))"
}

func inClause(field string, count int) string {
	if count <= 0 {
		return ""
	}
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return field + " IN (" + strings.Join(placeholders, ",") + ")"
}

func auditSearchClause(driverName string) string {
	if driverName == "mssql" || driverName == "sqlserver" {
		return ` WHERE
			LOWER(ISNULL(user_email, '')) LIKE ? OR
			LOWER(ISNULL(action, '')) LIKE ? OR
			LOWER(ISNULL(target, '')) LIKE ? OR
			LOWER(ISNULL(details, '')) LIKE ? OR
			LOWER(CONVERT(NVARCHAR(30), created_at, 120)) LIKE ?`
	}
	return ` WHERE
		LOWER(COALESCE(user_email, '')) LIKE ? OR
		LOWER(COALESCE(action, '')) LIKE ? OR
		LOWER(COALESCE(target, '')) LIKE ? OR
		LOWER(COALESCE(details, '')) LIKE ? OR
		LOWER(COALESCE(CAST(created_at AS TEXT), '')) LIKE ?`
}

// AuditListHandler returns paginated audit entries (GET /api/v1/audit).
func AuditListHandler(w http.ResponseWriter, r *http.Request) {
	draw := 0
	if rawDraw := r.URL.Query().Get("draw"); rawDraw != "" {
		if parsed, err := strconv.Atoi(rawDraw); err == nil && parsed >= 0 {
			draw = parsed
		}
	}

	limit := 20
	offset := 0
	if rawLength := r.URL.Query().Get("length"); rawLength != "" {
		parsed, err := strconv.Atoi(rawLength)
		if err != nil || parsed <= 0 {
			http.Error(w, "Invalid length", http.StatusBadRequest)
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	} else if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	if rawStart := r.URL.Query().Get("start"); rawStart != "" {
		parsed, err := strconv.Atoi(rawStart)
		if err != nil || parsed < 0 {
			http.Error(w, "Invalid start", http.StatusBadRequest)
			return
		}
		offset = parsed
	} else if rawOffset := r.URL.Query().Get("offset"); rawOffset != "" {
		parsed, err := strconv.Atoi(rawOffset)
		if err != nil || parsed < 0 {
			http.Error(w, "Invalid offset", http.StatusBadRequest)
			return
		}
		offset = parsed
	}

	search := strings.TrimSpace(r.URL.Query().Get("search[value]"))
	if search == "" {
		search = strings.TrimSpace(r.URL.Query().Get("search"))
	}

	users := splitCSVValues(r.URL.Query().Get("users"))
	actions := splitCSVValues(r.URL.Query().Get("actions"))

	dateFrom, hasDateFrom := parseDateParam(r.URL.Query().Get("date_from"))
	if raw := strings.TrimSpace(r.URL.Query().Get("date_from")); raw != "" && !hasDateFrom {
		http.Error(w, "Invalid date_from", http.StatusBadRequest)
		return
	}
	dateTo, hasDateTo := parseDateParam(r.URL.Query().Get("date_to"))
	if raw := strings.TrimSpace(r.URL.Query().Get("date_to")); raw != "" && !hasDateTo {
		http.Error(w, "Invalid date_to", http.StatusBadRequest)
		return
	}

	orderBy := "created_at"
	orderDir := "DESC"
	orderColumn := r.URL.Query().Get("order[0][column]")
	if orderColumn == "" {
		orderColumn = r.URL.Query().Get("orderColumn")
	}
	if orderColumn != "" {
		switch orderColumn {
		case "0":
			orderBy = "user_email"
		case "1":
			orderBy = "action"
		case "2":
			orderBy = "target"
		case "3":
			orderBy = "details"
		case "4":
			orderBy = "created_at"
		}
	}

	orderDirParam := r.URL.Query().Get("order[0][dir]")
	if orderDirParam == "" {
		orderDirParam = r.URL.Query().Get("orderDir")
	}
	if strings.EqualFold(orderDirParam, "asc") {
		orderDir = "ASC"
	}

	orderClause := fmt.Sprintf(" ORDER BY %s %s", orderBy, orderDir)

	database := db.Shared()

	var total int64
	if err := database.QueryRow("SELECT COUNT(*) FROM audit").Scan(&total); err != nil {
		http.Error(w, "DB count error", http.StatusInternalServerError)
		return
	}

	filteredTotal := total
	whereParts := make([]string, 0)
	filterArgs := make([]interface{}, 0)
	driverName := global.AppConfig.Database.Driver

	if len(users) > 0 {
		whereParts = append(whereParts, inClause(lowerExpr(driverName, "user_email"), len(users)))
		for _, u := range users {
			filterArgs = append(filterArgs, strings.ToLower(u))
		}
	}

	if len(actions) > 0 {
		whereParts = append(whereParts, inClause(lowerExpr(driverName, "action"), len(actions)))
		for _, a := range actions {
			filterArgs = append(filterArgs, strings.ToLower(a))
		}
	}

	if hasDateFrom {
		whereParts = append(whereParts, "created_at >= ?")
		filterArgs = append(filterArgs, dateFilterArg(driverName, dateFrom))
	}
	if hasDateTo {
		whereParts = append(whereParts, "created_at <= ?")
		filterArgs = append(filterArgs, dateFilterArg(driverName, dateTo))
	}

	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		whereParts = append(whereParts, strings.TrimPrefix(auditSearchClause(driverName), " WHERE "))
		filterArgs = append(filterArgs, like, like, like, like, like)
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = " WHERE " + strings.Join(whereParts, " AND ")
		countQuery := "SELECT COUNT(*) FROM audit" + whereClause
		if err := database.QueryRow(countQuery, filterArgs...).Scan(&filteredTotal); err != nil {
			http.Error(w, "DB filtered count error", http.StatusInternalServerError)
			return
		}
	}

	query := "SELECT id, user_email, action, target, details, ip_address, created_at FROM audit" + whereClause + orderClause + " LIMIT ? OFFSET ?"
	args := append(filterArgs, limit+1, offset)
	if global.AppConfig.Database.Driver == "mssql" || global.AppConfig.Database.Driver == "sqlserver" {
		query = fmt.Sprintf("SELECT id, user_email, action, target, details, ip_address, created_at FROM audit%s%s OFFSET ? ROWS FETCH NEXT ? ROWS ONLY", whereClause, orderClause)
		args = append(filterArgs, offset, limit+1)
	}

	rows, err := database.Query(query, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	audits := make([]schema.Audit, 0)
	for rows.Next() {
		var a schema.Audit
		if err := rows.Scan(&a.ID, &a.UserEmail, &a.Action, &a.Target, &a.Details, &a.IPAddress, &a.CreatedAt); err == nil {
			audits = append(audits, a)
		}
	}

	hasMore := len(audits) > limit
	if hasMore {
		audits = audits[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"items":    audits,
		"limit":    limit,
		"offset":   offset,
		"has_more": hasMore,
		"total":    total,
		"filtered": filteredTotal,
	}
	if draw > 0 {
		payload["draw"] = draw
		payload["recordsTotal"] = total
		payload["recordsFiltered"] = filteredTotal
		payload["data"] = audits
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// AuditFilterOptionsHandler returns distinct users/actions for audit filters.
func AuditFilterOptionsHandler(w http.ResponseWriter, r *http.Request) {
	database := db.Shared()

	users := make([]string, 0)
	userRows, err := database.Query("SELECT DISTINCT user_email FROM audit WHERE user_email IS NOT NULL AND user_email <> '' ORDER BY user_email")
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	for userRows.Next() {
		var v string
		if err := userRows.Scan(&v); err == nil {
			users = append(users, v)
		}
	}
	userRows.Close()

	actions := make([]string, 0)
	actionRows, err := database.Query("SELECT DISTINCT action FROM audit WHERE action IS NOT NULL AND action <> '' ORDER BY action")
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	for actionRows.Next() {
		var v string
		if err := actionRows.Scan(&v); err == nil {
			actions = append(actions, v)
		}
	}
	actionRows.Close()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users":   users,
		"actions": actions,
	})
}

// AuditExportCSVHandler exports audit rows as CSV and applies optional search filter.
func AuditExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	database := db.Shared()

	search := strings.TrimSpace(r.URL.Query().Get("search"))
	users := splitCSVValues(r.URL.Query().Get("users"))
	actions := splitCSVValues(r.URL.Query().Get("actions"))

	dateFrom, hasDateFrom := parseDateParam(r.URL.Query().Get("date_from"))
	if raw := strings.TrimSpace(r.URL.Query().Get("date_from")); raw != "" && !hasDateFrom {
		http.Error(w, "Invalid date_from", http.StatusBadRequest)
		return
	}
	dateTo, hasDateTo := parseDateParam(r.URL.Query().Get("date_to"))
	if raw := strings.TrimSpace(r.URL.Query().Get("date_to")); raw != "" && !hasDateTo {
		http.Error(w, "Invalid date_to", http.StatusBadRequest)
		return
	}

	whereParts := make([]string, 0)
	args := make([]interface{}, 0)
	driverName := global.AppConfig.Database.Driver

	if len(users) > 0 {
		whereParts = append(whereParts, inClause(lowerExpr(driverName, "user_email"), len(users)))
		for _, u := range users {
			args = append(args, strings.ToLower(u))
		}
	}

	if len(actions) > 0 {
		whereParts = append(whereParts, inClause(lowerExpr(driverName, "action"), len(actions)))
		for _, a := range actions {
			args = append(args, strings.ToLower(a))
		}
	}

	if hasDateFrom {
		whereParts = append(whereParts, "created_at >= ?")
		args = append(args, dateFilterArg(driverName, dateFrom))
	}
	if hasDateTo {
		whereParts = append(whereParts, "created_at <= ?")
		args = append(args, dateFilterArg(driverName, dateTo))
	}

	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		whereParts = append(whereParts, strings.TrimPrefix(auditSearchClause(driverName), " WHERE "))
		args = append(args, like, like, like, like, like)
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = " WHERE " + strings.Join(whereParts, " AND ")
	}

	query := "SELECT user_email, action, target, details, created_at FROM audit" + whereClause + " ORDER BY created_at DESC"
	rows, err := database.Query(query, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"audit_export.csv\"")

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"Email", "Action", "Target", "Details", "Date"})

	for rows.Next() {
		var email, action, target, details, createdAt string
		if err := rows.Scan(&email, &action, &target, &details, &createdAt); err != nil {
			continue
		}
		_ = cw.Write([]string{email, action, target, details, createdAt})
	}
}
