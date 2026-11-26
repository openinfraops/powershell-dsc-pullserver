# Release v1.8.0

Date: 2026-11-26

Feature release adding DSC report **error grouping** and **pending-reboot** state, plus **monitoring** endpoints (`/health`, `/metrics`). It also refactors the scheduled tasks into a registry, adds graceful shutdown on `SIGTERM`/`SIGINT` on non-Windows, hardens client-certificate validation when served over plain HTTP, and fixes the Linux build. CI now runs on a fully patched Go 1.26 toolchain so the `govulncheck` standard-library scan is clean.

## API changes

- *Additive:* `GET /api/v1/error_groups` — list and filter DSC report error groups (filters: `status`, `category`, `since`, `min_nodes`, `offset`, `limit`).
- *Additive:* `GET /api/v1/error_groups/{fingerprint}` — detail of a single error group, including its current detection status.
- *Additive:* `GET /api/v1/error_groups/{fingerprint}/nodes` — the nodes/occurrences contributing to a group (`offset`, `limit`).
- *Additive:* `PUT /api/v1/error_groups/{fingerprint}` — update a group's `status`/`note` (admin only; e.g. acknowledge or resolve a group).
- *Additive:* `GET /api/v1/agents` and `GET /api/v1/agents/{id}` — the agent list/detail responses now include the pending-reboot columns `reboot_pending`, `reboot_pending_since`, `reboot_requested_by`, `reboot_loop`, `reboot_request_count`. `GET /api/v1/agents` gains the `?reboot_pending=true|false` and `?reboot_loop=true` filters, and the `?stats` summary now also returns `reboot_pending` and `reboot_loop` counts.
- *Additive:* `GET /health` — unauthenticated liveness probe (for load balancers / orchestrators). Not under `/api/v1/`; no authentication.
- *Additive:* `GET /metrics` — Prometheus text-format metrics, gated by the new `web_ui.metrics_token`. Not under `/api/v1/`.

**No breaking changes.** These changes are backward-compatible: existing fields and endpoints are unchanged, the agent list/detail and stats responses only gain optional fields, and the new endpoints are additive. The `/health` and `/metrics` endpoints live outside `/api/v1/` and do not affect the provider-facing contract.

## Changes

### DSC report error classification and grouping

The server now classifies the errors reported by nodes (via `SendReport`), groups identical errors into a fingerprint, and lets operators triage them. Two new tables are created idempotently by the schema files:

- **`error_groups`** — one row per distinct error fingerprint (`fingerprint`, `category`, `resource_type`, `sample_message`, `first_seen`, `last_seen`, `first_config_version`, `status` [`open`/…], `status_changed_by`, `status_changed_at`, `note`, `last_alerted_at`).
- **`error_occurrences`** — per-node occurrence counts for a fingerprint (`fingerprint`, `node_id`, `first_seen`, `last_seen`, `count`, `last_report_id`).

Classification lives in `internal/schema/errorclassify.go`. Group status (open/acknowledged/resolved) is computed from the underlying occurrences, so a group opens when the last occurrence is recent and resolves once occurrences age out or an operator updates it via `PUT /api/v1/error_groups/{fingerprint}`. The `error_groups`/`error_occurrences` tables are created by re-running `db/schema_*.sql` (SQLite uses `CREATE TABLE IF NOT EXISTS`, MSSQL guards with `IF NOT EXISTS`), so **no dedicated migration script is needed** — the schema evolves idempotently at startup via `InitDB`.

### Pending-reboot state from DSC reports

The server now extracts the pending-reboot flag that nodes report (a common `RebootPending` / `RebootIfNeeded` signal in DSC reports) and stores it on the agent: `agents.reboot_pending`, `reboot_pending_since`, `reboot_requested_by`, `reboot_loop`, `reboot_request_count`. A node that keeps requesting a reboot across reports is flagged as `reboot_loop`, which operators can spot and filter on (`?reboot_loop=true`) to catch nodes that never actually reboot. The pending-reboot state is surfaced in `GET /api/v1/agents`, `GET /api/v1/agents/{id}`, and the `?stats` summary. Schema added via the new `agents` columns in `db/schema_*.sql`.

### Monitoring endpoints (`/health`, `/metrics`)

Two new endpoints help operate and observe the server:

- `GET /health` — lightweight, **unauthenticated** liveness probe. Returns `200` when the server can reach its database; returns `503` (with a `degraded` body excluding `dsc_nodes`) when the DB is unreachable.
- `GET /metrics` — **Prometheus** text-format metrics (`go_*` runtime + application metrics such as `dsc_nodes`, `dsc_reports_*`, `dsc_register_*`, `dsc_audit_*`, `dsc_scheduler_*`, `dsc_error_*`). Gated by the new `web_ui.metrics_token` config key; when unset, the endpoint returns `404` so metrics are not exposed by default.

### Scheduled tasks refactored into a registry

The background tasks were previously a flat set of functions wired directly in `main.go`. They are now registered in a **task registry** (`internal/service/scheduler.go` + `internal/service/scheduler_test.go`), so tasks are discoverable, individually testable, and each task has a stable name/summary used in `POST /api/v1/scheduler/*/run` and the task-history endpoint. The ad-hoc timer wiring in `main.go` was replaced by a single scheduler loop driven by the registry.

### Graceful shutdown on `SIGTERM` / `SIGINT`

On non-Windows platforms the server now listens for `SIGTERM`/`SIGINT` and performs a graceful shutdown: the HTTP server is drained via `http.Server.Shutdown` (with a timeout) before the process exits, so in-flight requests complete instead of being cut off. On Windows the existing SCM service behaviour is unchanged.

### Security hardening

- **Fail closed over HTTP.** When client-certificate validation is enabled (`dsc_pullserver.client_cert_validation = on`/`required`) but the server is served over plain HTTP, authentication can no longer be satisfied by a missing/empty client certificate. The check now fails closed, returning an error instead of silently accepting the connection. (On TLS, a real certificate is always required.)
- **Web login deny/blocked message.** The login page now shows an explicit French message when an account is denied or blocked, instead of a generic failure.
- **Log sink hardening.** `internal/logs` now writes to stderr as well as the log file (matching the `[GETDSCACTION-NODE]`/`[SENDREPORT]` behaviour) and handles concurrent writes safely.

### CI / build fixes

- **Linux build fixed.** `internal/service/service.go` was split into build-tagged files (`service_windows.go` for the SCM `service` package, `service_other.go` for non-Windows) so the Linux build no longer fails on `golang.org/x/sys/windows/svc`.
- **Fully patched Go toolchain.** `actions/setup-go` was raised `1.26.6` → `1.26.11` (the latest 1.26.x patch) so `govulncheck` reports zero standard-library findings (GO-2026-6859 `net/http`, GO-2026-6809 `crypto/x509`, and the earlier `html/template` advisories are all fixed). `go.mod` stays at `go 1.26.0` (a minimum).
- **Integration tests + CI security checks.** `handlers/dsc_integration_test.go` (and the `#82` changes) add DSC protocol integration tests, and CI now runs `govulncheck` and `gosec` in addition to the existing checks.

### Documentation

- [Security](dscpull/security.md) documents the fail-closed client-certificate behaviour over HTTP, the supported-versions / threat-model summary, and the security reporting policy.
- [API v1 reference](APIv1.md) adds the `error_groups` endpoints, the agent reboot fields/filters, and the non-versioned `/health` and `/metrics` endpoints.
- [Database schema](DatabaseSchema.md) documents the `error_groups`, `error_occurrences` tables and the new `agents.reboot_*` columns.
- [Configuration reference](Configuration.md) documents `web_ui.metrics_token`.
- [Integrations](Integrations.md) documents API stability and the cross-links with the Terraform provider.

## Impacted files

- `handlers/error_groups_api.go`, `handlers/error_groups_api_test.go`
- `handlers/agent_api.go`, `handlers/agent_byid_api.go`, `handlers/agent_datatable.go`
- `handlers/health_api.go`, `handlers/metrics_api.go`, `handlers/monitoring_test.go`
- `handlers/sendreport.go`, `handlers/sendreport_reboot_test.go`
- `handlers/scheduler_config_api.go`, `handlers/agent_reports_list.go`, `handlers/export_agents_csv.go`
- `internal/db/error_groups.go`, `internal/db/error_groups_test.go`
- `internal/db/reboot.go`, `internal/db/reboot_test.go`
- `internal/db/monitoring.go`, `internal/db/agent_schema_ensure.go`
- `internal/schema/errorclassify.go`, `internal/schema/errorclassify_test.go`, `internal/schema/agent.go`
- `internal/schema/flexbool.go`, `internal/schema/flexbool_test.go`, `internal/schema/appconfig.go`
- `internal/service/scheduler.go`, `internal/service/scheduler_test.go`
- `internal/service/service.go`, `internal/service/service_windows.go`, `internal/service/service_other.go`
- `internal/auth/security_check.go`, `internal/auth/security_check_test.go`
- `internal/monitor/monitor.go`, `internal/monitor/monitor_test.go`
- `internal/routes/web_routes.go`, `internal/routes/web_handlers.go`
- `internal/logs/logfile.go`
- `main.go`, `main_tasks.go`, `templates/login.tmpl`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `.github/workflows/ci.yml`
- `doc/APIv1.md`, `doc/DatabaseSchema.md`, `doc/Configuration.md`, `doc/Integrations.md`, `doc/dscpull/security.md`, `doc/dscpull/configuration.md`
