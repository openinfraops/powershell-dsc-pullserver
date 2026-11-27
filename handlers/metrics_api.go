package handlers

import (
	"crypto/subtle"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-dsc-pull/internal/buildinfo"
	internaldb "go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/monitor"
	"go-dsc-pull/utils"
)

// MetricsHandler returns the GET /metrics endpoint (Prometheus text format).
//
// The endpoint is gated by a token. A caller is accepted when it presents
// either:
//   - the configured web_ui.metrics_token (via an "Authorization: Bearer
//     <token>" header or a "?token=<token>" query parameter), or
//   - a valid user API token (the same "Authorization: Token <token>" used by
//     the /api/v1/* endpoints), so a single {TOKEN_API} can scrape both the
//     REST API and /metrics.
//
// No node names or secrets are exposed; the only label values are build
// version, compliance states, scheduler task keys and task statuses.
func MetricsHandler(dbConn *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validMetricsToken(r, metricsToken(), dbConn) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		renderMetrics(w, dbConn)
	})
}

// metricsToken reads the configured metrics token (web_ui.metrics_token).
func metricsToken() string {
	if global.AppConfig == nil {
		return ""
	}
	return global.AppConfig.WebUI.MetricsToken
}

// validMetricsToken reports whether the request presents a token accepted by
// /metrics. A request is accepted when either:
//   - the presented token matches web_ui.metrics_token (Bearer header or
//     ?token= query), or
//   - the presented token is a valid user API token (the same {TOKEN_API} used
//     by the /api/v1/* endpoints), so Zabbix/Prometheus can reuse one token.
func validMetricsToken(r *http.Request, expected string, dbConn *sql.DB) bool {
	provided := ""
	if auth := r.Header.Get("Authorization"); auth != "" {
		switch {
		case strings.HasPrefix(auth, "Bearer "):
			provided = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		case strings.HasPrefix(auth, "Token "):
			provided = strings.TrimSpace(strings.TrimPrefix(auth, "Token "))
		}
	}
	if provided == "" {
		provided = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if provided == "" {
		return false
	}

	// 1. Match against the configured metrics token (constant-time).
	if expected != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1 {
		return true
	}

	// 2. Fall back to a valid user API token, so the same {TOKEN_API} used for
	//    /api/v1/* can also scrape /metrics.
	if userId, err := internaldb.CheckAPIToken(dbConn, provided); err == nil && userId > 0 {
		return true
	}

	return false
}

// renderMetrics writes the Prometheus text exposition for the process.
func renderMetrics(w http.ResponseWriter, dbConn *sql.DB) {
	var b strings.Builder
	dbUp := true

	// Gather all DB-backed data first, so dbUp reflects whether the whole set of
	// queries succeeded before any metric is emitted. This avoids emitting a
	// metric family twice if one query fails after another succeeded.
	totalNodes := 0
	states := map[string]int{}
	var stalestAge int64
	if n, err := internaldb.CountAgents(dbConn); err == nil {
		totalNodes = n
	} else {
		dbUp = false
	}
	if st, err := internaldb.AgentsByState(dbConn); err == nil {
		states = st
	} else {
		dbUp = false
	}
	if age, err := internaldb.StalestLastReportAge(dbConn, time.Now()); err == nil {
		stalestAge = age
	} else {
		dbUp = false
	}
	var tasks []internaldb.SchedulerTaskInfo
	if t, err := internaldb.ListSchedulerTasks(dbConn); err == nil {
		tasks = t
	} else {
		dbUp = false
	}
	var rebootPending, rebootLoop int
	if n, err := internaldb.CountRebootPending(dbConn); err == nil {
		rebootPending = n
	} else {
		dbUp = false
	}
	if n, err := internaldb.CountRebootLoop(dbConn); err == nil {
		rebootLoop = n
	} else {
		dbUp = false
	}
	rc, rcErr := internaldb.GetReleaseCheckState(dbConn)
	if rcErr != nil {
		dbUp = false
	}

	// Error group metrics (derived from error_groups / error_occurrences).
	// Gathered here, before any emission, so a DB failure is reflected in
	// dsc_db_up and no partial metric family is written.
	errorGroupsByStatus := map[string]int{}
	errorGroupsNewByStatus := map[string]int{}
	// Always seed the "unknown" category so its sample (the generic / report
	// noise) is present even when no open group currently belongs to it; this
	// lets a Zabbix trigger subtract the noise without hitting an unsupported
	// item. The label value "unknown" mirrors DefaultErrorCategory.
	openGroupsByCategory := map[string]int{"unknown": 0}
	{
		statuses := []string{internaldb.ErrorGroupStatusOpen, internaldb.ErrorGroupStatusKnown, internaldb.ErrorGroupStatusResolved}
		for _, st := range statuses {
			if n, err := internaldb.CountErrorGroups(dbConn, internaldb.ErrorGroupFilter{Status: st}); err == nil {
				errorGroupsByStatus[st] = n
			} else {
				dbUp = false
			}
		}
		since := time.Now().UTC().Add(-24 * time.Hour)
		for _, st := range statuses {
			if n, err := internaldb.CountErrorGroups(dbConn, internaldb.ErrorGroupFilter{Status: st, Since: since}); err == nil {
				errorGroupsNewByStatus[st] = n
			} else {
				dbUp = false
			}
		}
		if groups, err := internaldb.ListErrorGroups(dbConn, internaldb.ErrorGroupFilter{Status: internaldb.ErrorGroupStatusOpen}); err == nil {
			for _, g := range groups {
				cat := strings.TrimSpace(g.Category)
				if cat == "" {
					cat = "unknown"
				}
				openGroupsByCategory[cat]++
			}
		} else {
			dbUp = false
		}
	}

	// Build info.
	writeMetricHeader(&b, "dsc_build_info", "DSC Pull Server build information.", "gauge")
	writeSample(&b, "dsc_build_info", "version="+promEscape(buildinfo.Version), 1)

	if dbUp {
		writeMetricHeader(&b, "dsc_db_up", "Whether the database is reachable (1=reachable, 0=not).", "gauge")
		writeSample(&b, "dsc_db_up", "", 1)

		writeMetricHeader(&b, "dsc_nodes", "Number of active (non-deleted) nodes.", "gauge")
		writeSample(&b, "dsc_nodes", "", float64(totalNodes))

		writeMetricHeader(&b, "dsc_nodes_by_state", "Number of nodes by compliance state.", "gauge")
		for state, count := range states {
			label := "unknown"
			if strings.TrimSpace(state) != "" {
				label = state
			}
			writeSample(&b, "dsc_nodes_by_state", "state="+promEscape(label), float64(count))
		}

		writeMetricHeader(&b, "dsc_last_report_age_seconds", "Age of the stalest node's last report, in seconds.", "gauge")
		writeSample(&b, "dsc_last_report_age_seconds", "", float64(stalestAge))

		writeMetricHeader(&b, "dsc_nodes_reboot_pending", "Number of active nodes currently pending a reboot.", "gauge")
		writeSample(&b, "dsc_nodes_reboot_pending", "", float64(rebootPending))
		writeMetricHeader(&b, "dsc_nodes_reboot_loop", "Number of active nodes flagged in a reboot loop.", "gauge")
		writeSample(&b, "dsc_nodes_reboot_loop", "", float64(rebootLoop))

		// Scheduler task metrics.
		writeMetricHeader(&b, "dsc_scheduler_task_last_status", "Last status of each scheduled task.", "gauge")
		writeMetricHeader(&b, "dsc_scheduler_task_last_run_timestamp_seconds", "Last run start of each scheduled task (Unix seconds).", "gauge")
		writeMetricHeader(&b, "dsc_scheduler_task_next_run_timestamp_seconds", "Next scheduled run of each task (Unix seconds).", "gauge")
		for _, t := range tasks {
			writeSample(&b, "dsc_scheduler_task_last_status",
				fmt.Sprintf("task=%s,status=%s", promEscape(t.TaskName), promEscape(t.LastStatus)), 1)
			if v, ok := parseDBTimestamp(t.LastRunAt); ok {
				writeSample(&b, "dsc_scheduler_task_last_run_timestamp_seconds", "task="+promEscape(t.TaskName), float64(v))
			}
			if v, ok := parseDBTimestamp(t.NextRunAt); ok {
				writeSample(&b, "dsc_scheduler_task_next_run_timestamp_seconds", "task="+promEscape(t.TaskName), float64(v))
			}
		}

		// Release check metrics.
		updateAvailable := 0
		if rc.UpdateAvailable {
			updateAvailable = 1
		}
		releaseOK := 0
		if rc.ReleaseCheckOK {
			releaseOK = 1
		}
		writeMetricHeader(&b, "dsc_update_available", "Whether a newer release is available (1=yes).", "gauge")
		writeSample(&b, "dsc_update_available", "", float64(updateAvailable))
		writeMetricHeader(&b, "dsc_release_check_ok", "Whether the last release check succeeded (1=yes).", "gauge")
		writeSample(&b, "dsc_release_check_ok", "", float64(releaseOK))

		// Error group metrics.
		writeMetricHeader(&b, "dsc_error_groups", "Number of distinct error groups by status.", "gauge")
		for _, st := range []string{internaldb.ErrorGroupStatusOpen, internaldb.ErrorGroupStatusKnown, internaldb.ErrorGroupStatusResolved} {
			writeSample(&b, "dsc_error_groups", "status="+promEscape(st), float64(errorGroupsByStatus[st]))
		}

		writeMetricHeader(&b, "dsc_error_groups_new_24h", "Number of error groups first seen in the last 24 hours, by status.", "gauge")
		for _, st := range []string{internaldb.ErrorGroupStatusOpen, internaldb.ErrorGroupStatusKnown, internaldb.ErrorGroupStatusResolved} {
			writeSample(&b, "dsc_error_groups_new_24h", "status="+promEscape(st), float64(errorGroupsNewByStatus[st]))
		}

		writeMetricHeader(&b, "dsc_error_groups_by_category", "Number of open error groups by category.", "gauge")
		cats := make([]string, 0, len(openGroupsByCategory))
		for cat := range openGroupsByCategory {
			cats = append(cats, cat)
		}
		sort.Strings(cats)
		for _, cat := range cats {
			writeSample(&b, "dsc_error_groups_by_category", "category="+promEscape(cat), float64(openGroupsByCategory[cat]))
		}
	} else {
		// Database is unreachable: report it and skip the DB-derived gauges.
		writeMetricHeader(&b, "dsc_db_up", "Whether the database is reachable (1=reachable, 0=not).", "gauge")
		writeSample(&b, "dsc_db_up", "", 0)
	}

	// HTTP response counters (401/403 are the security-relevant ones).
	writeMetricHeader(&b, "dsc_http_responses_total", "Total HTTP responses by status code.", "counter")
	writeSample(&b, "dsc_http_responses_total", `code="401"`, float64(monitor.StatusCount(http.StatusUnauthorized)))
	writeSample(&b, "dsc_http_responses_total", `code="403"`, float64(monitor.StatusCount(http.StatusForbidden)))

	// TLS certificate expiry (web server certificate).
	if cert, err := loadWebTLSCertificate(); err == nil && cert != nil {
		notAfter := cert.NotAfter
		writeMetricHeader(&b, "dsc_tls_cert_expiry_seconds", "Seconds until the web TLS certificate expires.", "gauge")
		writeSample(&b, "dsc_tls_cert_expiry_seconds", "", time.Until(notAfter).Seconds())
		writeMetricHeader(&b, "dsc_tls_cert_not_after_timestamp_seconds", "NotAfter timestamp of the web TLS certificate (Unix seconds).", "gauge")
		writeSample(&b, "dsc_tls_cert_not_after_timestamp_seconds", "", float64(notAfter.Unix()))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// writeMetricHeader emits the # HELP and # TYPE lines for a metric family.
func writeMetricHeader(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, typ)
}

// writeSample emits one labelled sample for a metric family.
func writeSample(b *strings.Builder, name, labels string, value float64) {
	b.WriteString(name)
	if labels != "" {
		b.WriteByte('{')
		b.WriteString(labels)
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	b.WriteByte('\n')
}

// promEscape escapes a label value and wraps it in double quotes, following the
// Prometheus text exposition escaping rules (backslash, double quote, newline).
func promEscape(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// parseDBTimestamp converts a "2006-01-02 15:04:05" timestamp (as returned by
// ListSchedulerTasks) into Unix seconds.
func parseDBTimestamp(s *string) (int64, bool) {
	if s == nil {
		return 0, false
	}
	t, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(*s))
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}

// loadWebTLSCertificate parses the configured web server certificate file and
// returns the leaf certificate, or (nil, nil) when no certificate is configured.
func loadWebTLSCertificate() (*x509.Certificate, error) {
	if global.AppConfig == nil {
		return nil, nil
	}
	certFile := strings.TrimSpace(global.AppConfig.WebUI.CertFile)
	if certFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(resolveCertPath(certFile))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in certificate file %q", certFile)
	}
	return x509.ParseCertificate(block.Bytes)
}

// resolveCertPath resolves a possibly-relative certificate path against the
// executable directory, mirroring the server's own cert/key resolution.
func resolveCertPath(certFile string) string {
	if filepath.IsAbs(certFile) {
		return certFile
	}
	if exe, err := utils.ExePath(); err == nil {
		return filepath.Join(filepath.Dir(exe), certFile)
	}
	return certFile
}
