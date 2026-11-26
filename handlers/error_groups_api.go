package handlers

import (
	"database/sql"
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
)

// errorGroupResponse is the JSON payload for a single error group, enriched with
// its detection flags and an HTML-escaped sample message.
type errorGroupResponse struct {
	Fingerprint         string     `json:"fingerprint"`
	Category            string     `json:"category"`
	ResourceType        string     `json:"resource_type"`
	SampleMessage       string     `json:"sample_message"`
	FirstSeen           *time.Time `json:"first_seen"`
	LastSeen            *time.Time `json:"last_seen"`
	FirstConfigVersion  string     `json:"first_config_version"`
	Status              string     `json:"status"`
	StatusChangedBy     string     `json:"status_changed_by"`
	StatusChangedAt     *time.Time `json:"status_changed_at"`
	Note                string     `json:"note"`
	NodeCount           int        `json:"node_count"`
	Spike               bool       `json:"spike"`
	New                 bool       `json:"new"`
	AfterDeployment     bool       `json:"after_deployment"`
	Trend               string     `json:"trend"`
	NodeCountLastHour   int        `json:"node_count_last_hour"`
	NodeCountPrevHour   int        `json:"node_count_prev_hour"`
	DetectionAlerted    bool       `json:"detection_alerted"`
	DetectionSuppressed bool       `json:"detection_suppressed"`
}

func timePtr(t time.Time, ok bool) *time.Time {
	if !ok || t.IsZero() {
		return nil
	}
	return &t
}

func toErrorGroupResponse(g db.ErrorGroup, det *db.DetectionResult) errorGroupResponse {
	r := errorGroupResponse{
		Fingerprint:        g.Fingerprint,
		Category:           g.Category,
		ResourceType:       g.ResourceType,
		SampleMessage:      html.EscapeString(g.SampleMessage),
		FirstSeen:          timePtr(g.FirstSeen, !g.FirstSeen.IsZero()),
		LastSeen:           timePtr(g.LastSeen, !g.LastSeen.IsZero()),
		FirstConfigVersion: g.FirstConfigVersion,
		Status:             g.Status,
		StatusChangedBy:    g.StatusChangedBy,
		StatusChangedAt:    timePtr(g.StatusChangedAt, !g.StatusChangedAt.IsZero()),
		Note:               g.Note,
		NodeCount:          g.NodeCount,
	}
	if det != nil {
		r.Spike = det.Spike
		r.New = det.New
		r.AfterDeployment = det.AfterDeployment
		r.Trend = det.Trend
		r.NodeCountLastHour = det.NodeCountLastHour
		r.NodeCountPrevHour = det.NodeCountPrevHour
		r.DetectionAlerted = det.Alerted
		r.DetectionSuppressed = det.Suppressed
	}
	return r
}

// errorDetectionParams computes the detection tuning knobs from configuration,
// applying the documented defaults.
func errorDetectionParams() db.ErrorDetectionParams {
	p := db.ErrorDetectionParams{
		Now:              time.Now().UTC(),
		SpikeMinNodes:    5,
		SpikePercent:     10,
		NewWindow:        24 * time.Hour,
		DeploymentWindow: 60 * time.Minute,
		GracePeriod:      5 * time.Minute,
	}
	if global.AppConfig != nil {
		c := global.AppConfig.DSCPullServer
		if c.ErrorSpikeMinNodes > 0 {
			p.SpikeMinNodes = c.ErrorSpikeMinNodes
		}
		if c.ErrorSpikePercent > 0 {
			p.SpikePercent = float64(c.ErrorSpikePercent)
		}
		if c.ErrorNewWindowHours > 0 {
			p.NewWindow = time.Duration(c.ErrorNewWindowHours) * time.Hour
		}
		if c.ErrorDeploymentWindowMins > 0 {
			p.DeploymentWindow = time.Duration(c.ErrorDeploymentWindowMins) * time.Minute
		}
		if c.ErrorDetectionRepeatMinutes > 0 {
			p.RepeatDelay = time.Duration(c.ErrorDetectionRepeatMinutes) * time.Minute
		}
		if c.ErrorDetectionGraceMinutes > 0 {
			p.GracePeriod = time.Duration(c.ErrorDetectionGraceMinutes) * time.Minute
		}
	}
	return p
}

// ErrorGroupsListHandler handles GET /api/v1/error_groups. Filters: status,
// category, since, min_nodes, tag (key:value, repeatable), limit, offset.
func ErrorGroupsListHandler(w http.ResponseWriter, r *http.Request) {
	database := db.Shared()
	f, err := parseErrorGroupFilter(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	groups, err := db.ListErrorGroups(database, f)
	if err != nil {
		logs.WriteLogFile("ErrorGroupsListHandler: DB query error: " + err.Error())
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	total, err := db.CountErrorGroups(database, f)
	if err != nil {
		logs.WriteLogFile("ErrorGroupsListHandler: DB count error: " + err.Error())
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}

	detMap := map[string]*db.DetectionResult{}
	if detections, derr := db.ComputeGroupDetection(database, errorDetectionParams()); derr == nil {
		for i := range detections {
			d := detections[i]
			detMap[d.Fingerprint] = &d
		}
	}

	responses := make([]errorGroupResponse, 0, len(groups))
	for _, g := range groups {
		responses = append(responses, toErrorGroupResponse(g, detMap[g.Fingerprint]))
	}

	out := map[string]interface{}{
		"groups": responses,
		"total":  total,
		"offset": f.Offset,
		"limit":  f.Limit,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ErrorGroupGetHandler handles GET /api/v1/error_groups/{fingerprint}.
func ErrorGroupGetHandler(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	database := db.Shared()
	g, err := db.GetErrorGroup(database, fingerprint)
	if err != nil {
		logs.WriteLogFile("ErrorGroupGetHandler: DB query error: " + err.Error())
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	if g == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var det *db.DetectionResult
	if detections, derr := db.ComputeGroupDetection(database, errorDetectionParams()); derr == nil {
		for i := range detections {
			if detections[i].Fingerprint == fingerprint {
				d := detections[i]
				det = &d
				break
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toErrorGroupResponse(*g, det))
}

// ErrorGroupNodesHandler handles GET /api/v1/error_groups/{fingerprint}/nodes.
func ErrorGroupNodesHandler(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	database := db.Shared()
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	occ, err := db.ListErrorGroupNodes(database, fingerprint, offset, limit)
	if err != nil {
		logs.WriteLogFile("ErrorGroupNodesHandler: DB query error: " + err.Error())
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	out := map[string]interface{}{
		"nodes":  occ,
		"offset": offset,
		"limit":  limit,
		"count":  len(occ),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ErrorGroupUpdateHandler handles PUT /api/v1/error_groups/{fingerprint} (admin
// only): set status known/resolved/open and add a note.
func ErrorGroupUpdateHandler(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}
	if req.Status == "" {
		http.Error(w, "status is required", http.StatusBadRequest)
		return
	}

	rows, err := db.UpdateErrorGroupStatus(database, fingerprint, req.Status, currentUserEmail(r, database), req.Note, time.Now())
	if err != nil {
		logs.WriteLogFile("ErrorGroupUpdateHandler: DB update error: " + err.Error())
		http.Error(w, "DB update error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	action := "error_group." + req.Status
	_ = db.InsertAudit(database, global.AppConfig.Database.Driver, currentUserEmail(r, database), action, "error_groups", "Set error group "+fingerprint+" to "+req.Status, auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies))
	w.WriteHeader(http.StatusNoContent)
}

// currentUserEmail resolves the email of the authenticated user from the request
// context (userId), best-effort. It falls back to the literal sub when the user
// cannot be resolved from the users table.
func currentUserEmail(r *http.Request, database *sql.DB) string {
	switch v := r.Context().Value("userId").(type) {
	case string:
		if v == "" {
			return ""
		}
		var email string
		if err := database.QueryRow(`SELECT email FROM users WHERE email = ? OR first_name = ?`, v, v).Scan(&email); err == nil && email != "" {
			return email
		}
		return v
	case int64:
		var email string
		if err := database.QueryRow(`SELECT email FROM users WHERE id = ?`, v).Scan(&email); err == nil {
			return email
		}
	case int:
		var email string
		if err := database.QueryRow(`SELECT email FROM users WHERE id = ?`, v).Scan(&email); err == nil {
			return email
		}
	}
	return ""
}

// parseErrorGroupFilter builds an ErrorGroupFilter from query parameters.
func parseErrorGroupFilter(r *http.Request) (db.ErrorGroupFilter, error) {
	f := db.ErrorGroupFilter{
		Status:   r.URL.Query().Get("status"),
		Category: r.URL.Query().Get("category"),
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		if t, ok := parseDateParam(raw); ok {
			f.Since = t
		} else {
			return f, &invalidParamError{"invalid since, expected YYYY-MM-DD or ISO datetime"}
		}
	}
	if raw := r.URL.Query().Get("min_nodes"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return f, &invalidParamError{"invalid min_nodes"}
		}
		f.MinNodes = n
	}
	f.Offset = parseIntDefault(r.URL.Query().Get("offset"), 0)
	f.Limit = parseIntDefault(r.URL.Query().Get("limit"), 20)
	if f.Limit > 200 {
		f.Limit = 200
	}
	if rawTags, ok := r.URL.Query()["tag"]; ok {
		f.Tags = map[string]string{}
		for _, rawTag := range rawTags {
			key, value, found := strings.Cut(rawTag, ":")
			if !found || key == "" || value == "" {
				return f, &invalidParamError{"invalid tag filter, expected tag=key:value"}
			}
			f.Tags[key] = value
		}
	}
	return f, nil
}

func parseIntDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}

type invalidParamError struct{ msg string }

func (e *invalidParamError) Error() string { return e.msg }
