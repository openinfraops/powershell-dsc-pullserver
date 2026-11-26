package db

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"go-dsc-pull/internal/schema"
)

// Error group status values.
const (
	ErrorGroupStatusOpen     = "open"
	ErrorGroupStatusKnown    = "known"
	ErrorGroupStatusResolved = "resolved"
)

// DefaultErrorFingerprintDailyLimit bounds the number of distinct fingerprints a
// single node can introduce per day, to prevent database flooding by a noisy
// node. 0 in configuration falls back to this value.
const DefaultErrorFingerprintDailyLimit = 1000

// ErrorGroup is one distinct problem identified by its fingerprint.
type ErrorGroup struct {
	Fingerprint        string
	Category           string
	ResourceType       string
	SampleMessage      string
	FirstSeen          time.Time
	LastSeen           time.Time
	FirstConfigVersion string
	Status             string
	StatusChangedBy    string
	StatusChangedAt    time.Time
	Note               string
	NodeCount          int
}

// ErrorOccurrence is one node's (fingerprint,node) counter, updated in place.
type ErrorOccurrence struct {
	Fingerprint  string
	NodeID       string
	FirstSeen    time.Time
	LastSeen     time.Time
	Count        int
	LastReportID int64
}

// ErrorGroupFilter narrows a ListErrorGroups query.
type ErrorGroupFilter struct {
	Status   string
	Category string
	Since    time.Time
	MinNodes int
	Tags     map[string]string // tag_key -> tag_value
	Offset   int
	Limit    int
}

// ErrorDetectionParams are the tuning knobs for the periodic detection task.
type ErrorDetectionParams struct {
	Now              time.Time
	SpikeMinNodes    int
	SpikePercent     float64
	NewWindow        time.Duration
	DeploymentWindow time.Duration
	RepeatDelay      time.Duration
	GracePeriod      time.Duration
}

// DetectionResult is the outcome of evaluating one error group.
type DetectionResult struct {
	Fingerprint        string
	Category           string
	ResourceType       string
	Status             string
	NodeCount          int
	NodeCountLastHour  int
	NodeCountPrevHour  int
	Spike              bool
	New                bool
	AfterDeployment    bool
	Trend              string // "rising" | "stable" | "falling"
	Alerted            bool   // an alert should be emitted (not dedup-suppressed)
	Suppressed         bool   // detection present but suppressed (repeat delay / grace)
	Message            string
	FirstSeen          time.Time
	LastSeen           time.Time
	FirstConfigVersion string
}

// groupRow is a materialised error_groups row used by ComputeGroupDetection so the
// main rows cursor is fully drained (and closed) before per-group queries run.
type groupRow struct {
	fingerprint   string
	category      string
	resourceType  string
	configVersion string
	status        string
	firstSeen     time.Time
	lastSeen      time.Time
	lastAlerted   time.Time
	nodeCount     int
}

// ErrorGroupWriteInput carries the report-derived inputs for the in-transaction
// error grouping write.
type ErrorGroupWriteInput struct {
	AgentID    string
	ReportID   int64
	Errors     []string
	StatusData []string
	Now        time.Time
	Rules      []schema.CategoryRule
	DailyLimit int
}

// ApplyErrorGroupsTx extracts the error messages from a DSC report and upserts
// the derived error groups + occurrences, in the same transaction as the report
// insert/update. If no error part is present the call is a no-op.
func ApplyErrorGroupsTx(tx *sql.Tx, in ErrorGroupWriteInput) error {
	parts := schema.ExtractReportErrorParts(in.Errors, in.StatusData)
	if len(parts) == 0 {
		return nil
	}
	if in.DailyLimit <= 0 {
		in.DailyLimit = DefaultErrorFingerprintDailyLimit
	}
	configVersion := currentActiveConfigVersionTx(tx)
	firstSeen := formatDBTime(in.Now.UTC())
	dayStart := time.Date(in.Now.Year(), in.Now.Month(), in.Now.Day(), 0, 0, 0, 0, time.UTC)

	for _, p := range parts {
		// Redact before storage; the fingerprint uses the same redacted text so
		// secrets never leak into the key and two occurrences with different
		// secrets still merge.
		msg := schema.RedactSecret(schema.TruncateMessage(p.Message))
		if msg == "" {
			continue
		}
		fp := schema.Fingerprint(p.ResourceType, in.AgentID, msg)
		category := schema.ClassifyError(in.Rules, msg)

		if err := upsertErrorGroup(tx, fp, category, p.ResourceType, msg, configVersion, firstSeen); err != nil {
			return err
		}
		skip, err := upsertErrorOccurrence(tx, fp, in.AgentID, in.ReportID, firstSeen, dayStart, in.DailyLimit)
		if err != nil {
			return err
		}
		if skip {
			// The node hit its daily fingerprint limit: do not create a new
			// group for a fingerprint this node has never seen before. The
			// group may already exist from another node; that is fine.
			if err := removeOrphanGroup(tx, fp); err != nil {
				return err
			}
		}
	}
	return nil
}

// upsertErrorGroup inserts or updates the group row. first_seen and
// first_config_version are kept from the first occurrence.
func upsertErrorGroup(tx *sql.Tx, fingerprint, category, resourceType, sampleMessage string, configVersion string, seen interface{}) error {
	res, err := tx.Exec(`UPDATE error_groups SET category=?, resource_type=?, sample_message=?, last_seen=?, first_config_version=COALESCE(NULLIF(first_config_version,''), ?) WHERE fingerprint=?`,
		category, resourceType, sampleMessage, seen, configVersion, fingerprint)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		_, err = tx.Exec(`INSERT INTO error_groups (fingerprint, category, resource_type, sample_message, first_seen, last_seen, first_config_version, status) VALUES (?,?,?,?,?,?,?,?)`,
			fingerprint, category, resourceType, sampleMessage, seen, seen, configVersion, ErrorGroupStatusOpen)
	}
	return err
}

// upsertErrorOccurrence updates an existing occurrence or inserts a new one.
// It returns skip=true (without inserting) when the node has already reached its
// daily fingerprint limit and this is a brand-new fingerprint for it.
func upsertErrorOccurrence(tx *sql.Tx, fingerprint, nodeID string, reportID int64, seen interface{}, dayStart time.Time, dailyLimit int) (bool, error) {
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM error_occurrences WHERE fingerprint=? AND node_id=?`, fingerprint, nodeID).Scan(&exists); err != nil {
		return false, err
	}
	if exists > 0 {
		_, err := tx.Exec(`UPDATE error_occurrences SET last_seen=?, count=count+1, last_report_id=? WHERE fingerprint=? AND node_id=?`,
			seen, reportID, fingerprint, nodeID)
		return false, err
	}
	// New occurrence for this node: enforce the per-node daily fingerprint cap.
	var daily int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM error_occurrences WHERE node_id=? AND first_seen>=?`, nodeID, formatDBTime(dayStart)).Scan(&daily); err != nil {
		return false, err
	}
	if daily >= dailyLimit {
		return true, nil
	}
	_, err := tx.Exec(`INSERT INTO error_occurrences (fingerprint, node_id, first_seen, last_seen, count, last_report_id) VALUES (?,?,?,?,1,?)`,
		fingerprint, nodeID, seen, seen, reportID)
	return false, err
}

// removeOrphanGroup deletes a group that was just inserted for a fingerprint the
// node could not record (daily limit reached), but only when no other node still
// references it. This avoids leaving an empty group behind.
func removeOrphanGroup(tx *sql.Tx, fingerprint string) error {
	var refs int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM error_occurrences WHERE fingerprint=?`, fingerprint).Scan(&refs); err != nil {
		return err
	}
	if refs == 0 {
		_, err := tx.Exec(`DELETE FROM error_groups WHERE fingerprint=?`, fingerprint)
		return err
	}
	return nil
}

// currentActiveConfigVersionTx returns the name of the latest active
// configuration version (name == original_name), or "" when none exists.
func currentActiveConfigVersionTx(tx *sql.Tx) string {
	var name string
	err := tx.QueryRow(`SELECT name FROM configuration_model WHERE name = original_name ORDER BY upload_date DESC LIMIT 1`).Scan(&name)
	if err != nil {
		return ""
	}
	return name
}

// LatestActiveConfig returns the latest active configuration version name and
// its upload (publish) time. ok is false when no active version exists.
func LatestActiveConfig(database *sql.DB) (version string, publish time.Time, ok bool, err error) {
	var raw interface{}
	err = database.QueryRow(`SELECT name, upload_date FROM configuration_model WHERE name = original_name ORDER BY upload_date DESC LIMIT 1`).Scan(&version, &raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", time.Time{}, false, nil
		}
		return "", time.Time{}, false, err
	}
	ts, parsed := parseDBTime(raw)
	if !parsed {
		return version, time.Time{}, true, nil
	}
	return version, ts, true, nil
}

// PurgeErrorOccurrences removes occurrences whose last_seen is older than the
// retention window (aligned with report retention). It returns the number of
// rows removed.
func PurgeErrorOccurrences(database *sql.DB, driver string, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, fmt.Errorf("retentionDays must be > 0")
	}
	driver = strings.ToLower(driver)
	var result sql.Result
	var err error
	switch driver {
	case "mssql", "sqlserver":
		result, err = database.Exec(`DELETE FROM error_occurrences WHERE last_seen < DATEADD(day, -?, `+NowExpr(driver)+`)`, retentionDays)
	default:
		result, err = database.Exec(`DELETE FROM error_occurrences WHERE datetime(last_seen) < datetime('now', ?)`, fmt.Sprintf("-%d days", retentionDays))
	}
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// PurgeEphemeralErrorOccurrences removes occurrences belonging to agents that
// were hard-deleted (no matching live agent row), keeping the node lists
// consistent after ephemeral node teardown. It returns the number of rows
// removed.
func PurgeEphemeralErrorOccurrences(database *sql.DB) (int64, error) {
	result, err := database.Exec(`DELETE FROM error_occurrences WHERE node_id NOT IN (SELECT agent_id FROM agents)`)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// RunErrorDetection evaluates every error group and returns its detection flags.
// Groups that are alert-worthy and not dedup-suppressed get their last_alerted_at
// updated (repeat-delay) and are flagged Alerted so the caller can write them to
// the audit log / notification sink.
func RunErrorDetection(database *sql.DB, p ErrorDetectionParams) ([]DetectionResult, error) {
	results, err := ComputeGroupDetection(database, p)
	if err != nil {
		return nil, err
	}
	for i := range results {
		if results[i].Alerted {
			if _, err := database.Exec(`UPDATE error_groups SET last_alerted_at=? WHERE fingerprint=?`, formatDBTime(p.Now.UTC()), results[i].Fingerprint); err != nil {
				return nil, err
			}
		}
	}
	return results, nil
}

// ComputeGroupDetection evaluates every error group and returns its detection
// flags without mutating any state (used by both the periodic task and the API).
func ComputeGroupDetection(database *sql.DB, p ErrorDetectionParams) ([]DetectionResult, error) {
	if p.Now.IsZero() {
		p.Now = time.Now().UTC()
	}
	totalNodes, err := CountAgents(database)
	if err != nil {
		return nil, err
	}
	spikeThreshold := p.SpikeMinNodes
	if p.SpikePercent > 0 {
		byPercent := int(math.Ceil(p.SpikePercent / 100 * float64(totalNodes)))
		if byPercent > spikeThreshold {
			spikeThreshold = byPercent
		}
	}
	latestVersion, latestPublish, hasLatest, err := LatestActiveConfig(database)
	if err != nil {
		return nil, err
	}

	lastHourCutoff := p.Now.Add(-time.Hour)
	prevHourCutoff := lastHourCutoff.Add(-time.Hour)

	rows, err := database.Query(`SELECT fingerprint, category, resource_type, first_seen, last_seen, first_config_version, status, last_alerted_at,
		(SELECT COUNT(DISTINCT o.node_id) FROM error_occurrences o WHERE o.fingerprint = error_groups.fingerprint) AS node_count
		FROM error_groups`)
	if err != nil {
		return nil, err
	}

	// Matérialise toutes les lignes et ferme le curseur avant d'évaluer chaque
	// groupe, car les requêtes suivantes (occurrenceCounts, grace) réutilisent le
	// même *sql.DB (et la même connexion pour une base ":memory:").
	groups := make([]groupRow, 0)
	for rows.Next() {
		var (
			fingerprint, category, resourceType, status string
			configVersion                               interface{}
			firstSeen, lastSeen, lastAlerted            interface{}
			nodeCount                                   int
		)
		if err := rows.Scan(&fingerprint, &category, &resourceType, &firstSeen, &lastSeen, &configVersion, &status, &lastAlerted, &nodeCount); err != nil {
			rows.Close()
			return nil, err
		}
		firstSeenT, _ := parseDBTime(firstSeen)
		lastSeenT, _ := parseDBTime(lastSeen)
		lastAlertedT, _ := parseDBTime(lastAlerted)
		configVersionS, _ := dbString(configVersion)
		groups = append(groups, groupRow{fingerprint, category, resourceType, configVersionS, status, firstSeenT, lastSeenT, lastAlertedT, nodeCount})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	results := make([]DetectionResult, 0, len(groups))
	for _, gr := range groups {
		fingerprint := gr.fingerprint
		category := gr.category
		resourceType := gr.resourceType
		configVersion := gr.configVersion
		status := gr.status
		firstSeenT := gr.firstSeen
		lastSeenT := gr.lastSeen
		lastAlertedT := gr.lastAlerted
		nodeCount := gr.nodeCount

		lastHour, prevHour, err := occurrenceCounts(database, fingerprint, lastHourCutoff, prevHourCutoff)
		if err != nil {
			return nil, err
		}

		spike := lastHour >= spikeThreshold
		newFlag := !firstSeenT.IsZero() && p.Now.Sub(firstSeenT) <= p.NewWindow
		afterDeployment := false
		if hasLatest {
			if configVersion != "" && configVersion == latestVersion {
				afterDeployment = true
			} else if !firstSeenT.IsZero() && !firstSeenT.Before(latestPublish) && firstSeenT.Before(latestPublish.Add(p.DeploymentWindow)) {
				afterDeployment = true
			}
		}
		trend := "stable"
		if lastHour > prevHour {
			trend = "rising"
		} else if lastHour < prevHour {
			trend = "falling"
		}

		alertable := status != ErrorGroupStatusResolved && (spike || newFlag || afterDeployment)
		suppressed := false
		if alertable {
			// Grace period: transient errors right after a node start should not
			// raise a "new" alert.
			if newFlag && inGracePeriod(database, fingerprint, firstSeenT, p.GracePeriod) {
				suppressed = true
			}
			// Repeat delay: do not re-alert the same group too often.
			if !suppressed && !lastAlertedT.IsZero() && p.Now.Sub(lastAlertedT) < p.RepeatDelay {
				suppressed = true
			}
		}

		dr := DetectionResult{
			Fingerprint:        fingerprint,
			Category:           category,
			ResourceType:       resourceType,
			Status:             status,
			NodeCount:          nodeCount,
			NodeCountLastHour:  lastHour,
			NodeCountPrevHour:  prevHour,
			Spike:              spike,
			New:                newFlag,
			AfterDeployment:    afterDeployment,
			Trend:              trend,
			Alerted:            alertable && !suppressed,
			Suppressed:         alertable && suppressed,
			FirstSeen:          firstSeenT,
			LastSeen:           lastSeenT,
			FirstConfigVersion: configVersion,
		}
		if dr.Alerted {
			dr.Message = detectionMessage(dr, latestVersion)
		}
		results = append(results, dr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// occurrenceCounts returns the number of distinct nodes that touched a group in
// the last hour and in the previous hour.
func occurrenceCounts(database *sql.DB, fingerprint string, lastHourCutoff, prevHourCutoff time.Time) (int, int, error) {
	var lastHour, prevHour int
	err := database.QueryRow(`SELECT COUNT(DISTINCT node_id) FROM error_occurrences WHERE fingerprint=? AND last_seen>=?`,
		fingerprint, formatDBTime(lastHourCutoff)).Scan(&lastHour)
	if err != nil {
		return 0, 0, err
	}
	err = database.QueryRow(`SELECT COUNT(DISTINCT node_id) FROM error_occurrences WHERE fingerprint=? AND last_seen>=? AND last_seen<?`,
		fingerprint, formatDBTime(prevHourCutoff), formatDBTime(lastHourCutoff)).Scan(&prevHour)
	if err != nil {
		return 0, 0, err
	}
	return lastHour, prevHour, nil
}

// inGracePeriod reports whether the first offending node registered within the
// grace period before the group's first_seen, indicating a transient
// just-started node rather than a real regression.
func inGracePeriod(database *sql.DB, fingerprint string, firstSeen time.Time, grace time.Duration) bool {
	if grace <= 0 || firstSeen.IsZero() {
		return false
	}
	var minReg interface{}
	err := database.QueryRow(`SELECT MIN(a.registered_at) FROM error_occurrences o JOIN agents a ON a.agent_id = o.node_id WHERE o.fingerprint=?`,
		fingerprint).Scan(&minReg)
	if err != nil || minReg == nil {
		return false
	}
	reg, ok := parseDBTime(minReg)
	if !ok {
		return false
	}
	return firstSeen.Sub(reg) < grace
}

// detectionMessage builds a human-readable alert message.
func detectionMessage(dr DetectionResult, latestVersion string) string {
	parts := []string{}
	if dr.Spike {
		parts = append(parts, fmt.Sprintf("spike (%d nodes/h)", dr.NodeCountLastHour))
	}
	if dr.New {
		parts = append(parts, "new")
	}
	if dr.AfterDeployment {
		if latestVersion != "" {
			parts = append(parts, "appeared after version "+latestVersion)
		} else {
			parts = append(parts, "appeared after deployment")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "detected")
	}
	return fmt.Sprintf("Error group %s (%s): %s", dr.Fingerprint, dr.Category, strings.Join(parts, ", "))
}

// ListErrorGroups returns groups matching the filter, ordered by node count then
// most recently seen.
func ListErrorGroups(database *sql.DB, f ErrorGroupFilter) ([]ErrorGroup, error) {
	q := `SELECT g.fingerprint, g.category, g.resource_type, g.sample_message, g.first_seen, g.last_seen,
		g.first_config_version, g.status, g.status_changed_by, g.status_changed_at, g.note,
		(SELECT COUNT(DISTINCT o.node_id) FROM error_occurrences o WHERE o.fingerprint = g.fingerprint) AS node_count
		FROM error_groups g WHERE 1=1`
	args := []interface{}{}

	if f.Status != "" {
		q += " AND g.status = ?"
		args = append(args, f.Status)
	}
	if f.Category != "" {
		q += " AND g.category = ?"
		args = append(args, f.Category)
	}
	if !f.Since.IsZero() {
		q += " AND g.first_seen >= ?"
		args = append(args, formatDBTime(f.Since))
	}
	if f.MinNodes > 0 {
		q += " AND (SELECT COUNT(DISTINCT o.node_id) FROM error_occurrences o WHERE o.fingerprint = g.fingerprint) >= ?"
		args = append(args, f.MinNodes)
	}
	for key, value := range f.Tags {
		q += " AND EXISTS (SELECT 1 FROM agent_tags t JOIN error_occurrences o ON o.node_id = t.agent_id WHERE o.fingerprint = g.fingerprint AND t.tag_key = ? AND t.tag_value = ?)"
		args = append(args, key, value)
	}

	q += " ORDER BY node_count DESC, g.last_seen DESC"

	if f.Limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}

	rows, err := database.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []ErrorGroup{}
	for rows.Next() {
		var (
			g                                      ErrorGroup
			firstSeen, lastSeen, statusChangedAt   interface{}
			statusChangedBy, note, firstCfgVersion interface{}
		)
		if err := rows.Scan(&g.Fingerprint, &g.Category, &g.ResourceType, &g.SampleMessage, &firstSeen, &lastSeen,
			&firstCfgVersion, &g.Status, &statusChangedBy, &statusChangedAt, &note, &g.NodeCount); err != nil {
			return nil, err
		}
		g.FirstSeen, _ = parseDBTime(firstSeen)
		g.LastSeen, _ = parseDBTime(lastSeen)
		g.StatusChangedAt, _ = parseDBTime(statusChangedAt)
		g.StatusChangedBy, _ = dbString(statusChangedBy)
		g.Note, _ = dbString(note)
		g.FirstConfigVersion, _ = dbString(firstCfgVersion)
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// GetErrorGroup returns a single group by fingerprint, or nil when absent.
func GetErrorGroup(database *sql.DB, fingerprint string) (*ErrorGroup, error) {
	q := `SELECT g.fingerprint, g.category, g.resource_type, g.sample_message, g.first_seen, g.last_seen,
		g.first_config_version, g.status, g.status_changed_by, g.status_changed_at, g.note,
		(SELECT COUNT(DISTINCT o.node_id) FROM error_occurrences o WHERE o.fingerprint = g.fingerprint) AS node_count
		FROM error_groups g WHERE g.fingerprint = ?`
	rows, err := database.Query(q, fingerprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var (
		g                                      ErrorGroup
		firstSeen, lastSeen, statusChangedAt   interface{}
		statusChangedBy, note, firstCfgVersion interface{}
	)
	if err := rows.Scan(&g.Fingerprint, &g.Category, &g.ResourceType, &g.SampleMessage, &firstSeen, &lastSeen,
		&firstCfgVersion, &g.Status, &statusChangedBy, &statusChangedAt, &note, &g.NodeCount); err != nil {
		return nil, err
	}
	g.FirstSeen, _ = parseDBTime(firstSeen)
	g.LastSeen, _ = parseDBTime(lastSeen)
	g.StatusChangedAt, _ = parseDBTime(statusChangedAt)
	g.StatusChangedBy, _ = dbString(statusChangedBy)
	g.Note, _ = dbString(note)
	g.FirstConfigVersion, _ = dbString(firstCfgVersion)
	return &g, rows.Err()
}

// ListErrorGroupNodes returns the affected nodes (with their counters) for a
// group, most recently seen first.
func ListErrorGroupNodes(database *sql.DB, fingerprint string, offset, limit int) ([]ErrorOccurrence, error) {
	q := `SELECT fingerprint, node_id, first_seen, last_seen, count, last_report_id FROM error_occurrences WHERE fingerprint = ? ORDER BY last_seen DESC, node_id ASC`
	args := []interface{}{fingerprint}
	if limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := database.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	occ := []ErrorOccurrence{}
	for rows.Next() {
		var (
			o                   ErrorOccurrence
			firstSeen, lastSeen interface{}
		)
		if err := rows.Scan(&o.Fingerprint, &o.NodeID, &firstSeen, &lastSeen, &o.Count, &o.LastReportID); err != nil {
			return nil, err
		}
		o.FirstSeen, _ = parseDBTime(firstSeen)
		o.LastSeen, _ = parseDBTime(lastSeen)
		occ = append(occ, o)
	}
	return occ, rows.Err()
}

// UpdateErrorGroupStatus sets a group's status to known/resolved (or back to
// open) and records who changed it and when, plus an optional note. Returns the
// number of affected rows.
func UpdateErrorGroupStatus(database *sql.DB, fingerprint, status, changedBy, note string, now time.Time) (int64, error) {
	status = strings.TrimSpace(status)
	switch status {
	case ErrorGroupStatusOpen, ErrorGroupStatusKnown, ErrorGroupStatusResolved:
	default:
		return 0, fmt.Errorf("invalid status %q", status)
	}
	res, err := database.Exec(`UPDATE error_groups SET status=?, status_changed_by=?, status_changed_at=?, note=? WHERE fingerprint=?`,
		status, nullIfEmpty(changedBy), formatDBTime(now.UTC()), nullIfEmpty(note), fingerprint)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountErrorGroups returns the total number of groups matching the filter.
func CountErrorGroups(database *sql.DB, f ErrorGroupFilter) (int, error) {
	q := `SELECT COUNT(*) FROM error_groups g WHERE 1=1`
	args := []interface{}{}
	if f.Status != "" {
		q += " AND g.status = ?"
		args = append(args, f.Status)
	}
	if f.Category != "" {
		q += " AND g.category = ?"
		args = append(args, f.Category)
	}
	if !f.Since.IsZero() {
		q += " AND g.first_seen >= ?"
		args = append(args, formatDBTime(f.Since))
	}
	if f.MinNodes > 0 {
		q += " AND (SELECT COUNT(DISTINCT o.node_id) FROM error_occurrences o WHERE o.fingerprint = g.fingerprint) >= ?"
		args = append(args, f.MinNodes)
	}
	for key, value := range f.Tags {
		q += " AND EXISTS (SELECT 1 FROM agent_tags t JOIN error_occurrences o ON o.node_id = t.agent_id WHERE o.fingerprint = g.fingerprint AND t.tag_key = ? AND t.tag_value = ?)"
		args = append(args, key, value)
	}
	var n int
	err := database.QueryRow(q, args...).Scan(&n)
	return n, err
}

// dbString safely converts a nullable DB value to a string.
func dbString(v interface{}) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return fmt.Sprintf("%v", t), true
	}
}
