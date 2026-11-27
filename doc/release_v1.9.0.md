# Release v1.9.0

Date: 2026-11-27

Minor release. This release adds a re-registration policy for persistent
nodes, enforces admin-only pre-enrollment and tag operations with full audit,
forwards application logs and audit events to a syslog server, ships an
opt-in scheduled index-maintenance task, and significantly extends the
Prometheus/Zabbix monitoring (database connection-pool gauges and per-scheduled
task discovery + status alerting). It also fixes the configuration-version
pruning foreign-key failure on MSSQL and a nil-database panic in the async
node recompile path, and translates the console UI to English.

## API changes

### Additive — new endpoints

All new `/api/v1` endpoints below are **admin-only**:

- `PUT /api/v1/agents/{id}/persistent` — mark a node as persistent so the
  re-registration policy applies to it.
- `POST /api/v1/agents/{id}/approve-reregistration` — approve a pending
  re-registration for a persistent node.
- `POST /api/v1/agents/{id}/reregistration-window` — open a re-registration
  window for a persistent node.
- `GET /api/v1/agents/{id}/audit` — return the audit entries that reference a
  node (powers the "Modification History" panel on the node detail page).
- `POST /api/v1/scheduler/index-maintenance/run` — manually trigger the
  index-maintenance task.

### Additive — new fields on existing endpoints

- `PUT /api/v1/dsc/settings` gains `reregistration_mode`
  (`deny` | `approval` | `proof` | `window` | `allow`, default `deny`) and the
  `agents` section (`persistent`).
- `PUT /api/v1/scheduler/config` gains `enable_index_maintenance`,
  `index_maintenance_interval_mins` and `index_maintenance_vacuum`.

### Breaking — auth tightening

- `POST /api/v1/agents/preenroll` is now **admin-only** (previously any
  authenticated user could pre-enroll, and could set `environment=prod`). A
  non-admin token now receives `401`/`403`. Any client that pre-enrolls with a
  `user`-role token must switch to an **admin** token.

### Notes (Prometheus `/metrics`, additive)

`/metrics` exposes new gauge families (all additive, no label/name removals):
`dsc_error_groups{status}`, `dsc_error_groups_new_24h{status}`,
`dsc_error_groups_by_category`, and the connection-pool gauges
`dsc_db_pool_open_connections`, `dsc_db_pool_in_use`, `dsc_db_pool_idle`,
`dsc_db_pool_max_open`, `dsc_db_pool_wait_count_total`,
`dsc_db_pool_wait_duration_seconds_total`.

## Changes

### Re-registration policy for persistent nodes (#87)

Nodes can now be marked **persistent** so that a lost registration (a new
certificate, or a re-registration after a machine re-image) is governed by an
explicit policy instead of being rejected or silently accepted. The policy is
controlled globally by `dsc_settings.reregistration_mode`:

- `deny` (default) — a persistent node cannot be re-registered with a different
  identity.
- `approval` — a re-registration is held for an admin to approve.
- `proof` — a re-registration is accepted only if the incoming certificate
  thumbprint matches the one previously enrolled; otherwise it is rejected.
- `window` — a re-registration is accepted only while the admin has opened a
  temporary window.
- `allow` — re-registration is always accepted.

The mode only applies to **existing persistent nodes**; new nodes are not
affected. The mode is set via `PUT /api/v1/dsc/settings`, and per-node control
is exposed through `persistent`, `approve-reregistration` and
`reregistration-window`.

### Enforce admin-only pre-enrollment and tag changes, with audit (#3916130)

`POST /api/v1/agents/preenroll` is now enforced **admin-only** server-side
(the route is wrapped in `adminOnly` and the handler re-checks
`auth.IsAdmin`), because pre-enrollment can supply tags (including
`environment`). `PUT`/`DELETE /api/v1/agents/{id}/tags` and
`PUT /api/v1/agents/bulk/tags` were already admin-only and are now confirmed;
a node still cannot self-assign `environment` at registration (the DSC
registration endpoint never parses or stores tags). Every tag change and every
pre-enrollment is now written to the audit trail (actions `tag.set`,
`tag.delete`, `preenroll`) with before/after value, actor and source IP, with
emphasis on the `environment` tag.

### Audit identity overwrites and node-name collisions at registration (#79e6aab)

The registration path now detects and audits identity changes instead of
silently accepting them:

- `agent.overwrite` — an existing `agent_id` re-registers with a different
  certificate thumbprint (renewal, bad clone or takeover). Records old and new
  thumbprint, node name, registration type (policy) and source IP.
- `agent.collision` — a `node_name` is claimed by a second live identity
  (different `agent_id`, different thumbprint, still active within a 5-minute
  window). Records both identities, node name and source IP.

The actor is the node itself (`node:<agent_id>`). Detection runs in the
registration handler before the upsert so the old thumbprint is read in its
pre-change state. Late reports are unaffected: `SendReport` never creates
agent rows, so a superseded identity cannot create a duplicate node.

### Node modification history on the detail page (#ff3294f)

The node detail page (`/web/node/{id}`) gains a **Modification History** panel
showing the audit entries that mention the node. It is backed by the new
admin-only `GET /api/v1/agents/{id}/audit` (last 100 entries, most recent
first) and is gated to admins in the UI.

### Optional syslog forwarding of logs and audit events (#d474b06)

An optional, non-blocking syslog output (RFC 5424) can be configured in
`config.json` to forward application log records and audit events to a remote
syslog server in addition to the local file. It is **disabled by default** so
existing installs are unaffected. Supported transports are `udp`, `tcp` and
`tcp+tls` (RFC 5425 recommended); the queue is bounded and non-blocking
(drop-oldest + counter + throttled WARN) with automatic reconnection using
exponential backoff (500 ms → 30 s). Facility, `app_name` and `min_level` are
configurable; audit events are emitted as a distinct stream
(`MSGID=AUDIT`, `SD-ID audit@48577`) and always forwarded regardless of
`min_level`. New `doc/syslog.md` documents it with rsyslog and Zabbix examples.

### Scheduled index-maintenance task (#22b0d09)

Adds a driver-aware **Index maintenance** scheduled task (default **off**) that
optimises indexes once per day:

- **SQLite** — `PRAGMA optimize` + `REINDEX`, plus an optional `VACUUM`
  (`index_maintenance_vacuum`) to reclaim free pages. `VACUUM` requires an
  exclusive lock and can stall DSC traffic, so it stays opt-in.
- **MSSQL** — online `ALTER INDEX ... REORGANIZE` per user index (low-lock);
  never `REBUILD`.

Wired end-to-end like the other maintenance tasks: config keys
(`enable_index_maintenance` / `index_maintenance_interval_mins` /
`index_maintenance_vacuum`), `GET`/`PUT /api/v1/scheduler/config`, the
admin-only manual run endpoint `POST /api/v1/scheduler/index-maintenance/run`,
the Scheduler tasks/history listing, the Scheduler web UI modal, and docs.

### Performance: index the certificate-thumbprint lookup (#73)

Adds `idx_agents_certificate_thumbprint` on the agents certificate-thumbprint
column so the DSC middleware's thumbprint lookup is index-friendly (an
expensive full scan previously). The index is created idempotently
(`IF NOT EXISTS`) in `db/schema_sqlite.sql`, `db/schema_mssql.sql` and
`internal/db/schema.sql`, so existing databases pick it up at startup.

### Monitoring: richer Prometheus metrics and a bundled Zabbix template

`/metrics` now exposes DB connection-pool gauges from `sql.DB.Stats()`
(`dsc_db_pool_*`) and error-group gauges (`dsc_error_groups`,
`dsc_error_groups_new_24h`, `dsc_error_groups_by_category`; the `unknown`
category is always seeded so the Zabbix noise item is never unsupported).
The bundled Zabbix 7.0 template (`monitoring/zabbix_template.yml`, also
shipped in the release zip) now includes:

- the scheduled-task item `dsc.console.scheduler.tasks` (reads
  `/api/v1/scheduler/tasks`, **admin** token) and a `DSC Scheduler Task
  Discovery` rule creating one item per task with its last status, plus a
  per-task `HIGH` failure trigger prototype;
- dependent DB-pool items and triggers for pool exhaustion
  (`in_use >= max_open`), connection wait (wait-counter change) and saturation
  (`idle = 0` for 5 min while in use).

Because `/api/v1/scheduler/tasks` is admin-only, `{$TOKEN_API}` must now belong
to an **admin** user. The template also fixes the `triggers` block placement so
it imports correctly (it was a top-level child of `zabbix_export`, not inside
the template).

### Configuration: config file resolved next to the executable (#a8658ca)

`config.json` is now resolved relative to the **executable**, so a Windows
service (started by the SCM from `System32`) finds its configuration whatever
the working directory is. An `--config`/`-config` flag (or the `DSC_CONFIG`
environment variable) overrides the location; an absolute path is used as-is, a
relative one is resolved next to the executable. `--config <path>
init-bootstrap-admin` also works.

### Configuration: HTTP server timeouts configurable (#cbbd3ad)

The DSC and WebUI `http.Server` timeouts (previously hard-coded
10/120/180/120) are now configurable per server block with the same sensible
defaults, via `read_header_timeout_seconds`, `read_timeout_seconds`,
`write_timeout_seconds` and `idle_timeout_seconds`.

### Reliability: clean shutdown on listener errors, no more `os.Exit` (#a23b736)

The HTTP listener goroutines no longer call `os.Exit(1)` on
`ListenAndServe(TLS)` failure (which skipped deferred cleanups and the graceful
shutdown of the HTTP servers, DB pool, workers and log flush). `runApp` now
returns an error and listeners report their error on a buffered channel;
`runApp` selects on `ctx.Done()` and the error channel, runs the clean
shutdown, then returns the error so `main` exits non-zero. The Windows service
path returns a non-zero SCM exit code on failure. Fixes the port-in-use case.

### Fix: configuration-version pruning violated `previous_id` FK on MSSQL (#7706ee9)

The "Configuration version retention" task failed on MSSQL with a
`SAME TABLE REFERENCE constraint` because `configuration_model.previous_id`
is a self-referencing foreign key (NO ACTION). The prune now breaks the chain
before deleting, in a transaction: `UPDATE ... SET previous_id = NULL WHERE
previous_id IN (rows to delete)`, then `DELETE`. Works on both SQLite and MSSQL
and keeps the most recent history chain intact.

### Fix: guard against nil shared DB in async node recompile (#255eba1)

`triggerNodeRecompileAsync` runs compilation in a background goroutine that
reads `db.Shared()`, which may be nil when the goroutine runs (e.g. after test
teardown or before the app has initialised the shared connection). It now
returns early instead of panicking.

### Fix: satisfy gosec G402 for the syslog TLS transport (#e00dc73)

The syslog TLS transport honours an explicit `insecure_skip_verify` setting
that the operator opts into (never enabled implicitly, already logged as a
startup WARN). A justified `#nosec G402` is added so gosec CI passes.

### UI: console labels translated to English (#8b84d1c, #50e8bdc)

A complete sweep of the console UI to English — templates, JS labels, tooltips,
empty states and modal confirmations — so the UI no longer mixes French and
English. French comments in the source are left untouched.

### CI: read-only permissions on workflows (#15)

`ci.yml` and `docs.yml` now declare an explicit `permissions: contents: read`
so the CI jobs have no implicit write scope. `release.yml` keeps
`contents: write` (it creates the GitHub release and uploads assets).

### Documentation (#3125970, #88e6db3)

Adds a dedicated audit-trail page (`doc/Audit.md`, English) covering audited
events, stored fields, storage/querying, retention and access control, plus a
review of nine follow-up gaps. Updates the syslog docs and fixes a broken
relative link in `doc/syslog.md`.

## Impacted files

- `handlers/agent_registration.go`, `handlers/reregistration_policy.go`,
  `handlers/agent_persistent_api.go`, `handlers/dsc_settings_api.go`,
  `handlers/agents_tags.go`,
  `handlers/agent_audit_api.go`, `handlers/scheduler_config_api.go`,
  `handlers/metrics_api.go`, `handlers/health_api.go`
- `internal/db/index_maintenance.go`, `internal/db/scheduler_history.go`,
  `internal/db/schema.sql`, `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `internal/schema/appconfig.go`, `internal/schema/errorclassify.go`
- `main.go`, `main_tasks.go`, `bootstrap_admin.go`,
  `internal/routes/web_routes.go`, `internal/middleware/*`
- `internal/buildinfo/buildinfo.go`
- `monitoring/zabbix_template.yml`
- `templates/*.tmpl`, `web/*.js`
- `config_example.json`, `build/version.json`
- `.github/workflows/ci.yml`, `.github/workflows/docs.yml`,
  `.github/workflows/release.yml`
- `doc/APIv1.md`, `doc/Audit.md`, `doc/syslog.md`,
  `doc/Integrations.md`, `doc/Configuration.md`, `doc/installation.md`,
  `doc/DatabaseSchema.md`, `doc/dscpull/*.md`, `doc/README.md`
- `SECURITY.md`
- `doc/release_v1.9.0.md`
