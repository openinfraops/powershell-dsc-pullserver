# Audit trail

The server records security-relevant actions in an `audit` table and (when the
[syslog output](syslog.md) is enabled) forwards each event to a remote syslog
server as a structured stream. This page documents exactly what is recorded,
where it is stored, how to query it, how long it is retained, and who can read
it. It also records the outcome of a review performed against the feature, with
the identified gaps and their follow-up issues.

---

## Storage

Audit events are stored in the `audit` table (created in
`db/migration_v0.0.6_to_v1.0.0.sql`). Every event is written by
`db.InsertAudit(...)`, which also forwards the entry to syslog when the syslog
output is enabled (`MSGID = AUDIT`, structured data `audit@48577`).

| Column       | Type          | Description                                       |
|--------------|---------------|---------------------------------------------------|
| `id`         | integer (PK)  | Auto-incremented row id.                          |
| `user_email` | varchar(255)  | Actor email (nullable; empty for system/node actions). |
| `action`     | varchar(128)  | Action type: `create`, `update`, `delete`, `login`, `trash`, `restore`, `revert`, `run`, `preenroll`, `error_group.<status>`, … |
| `target`     | varchar(255)  | Object type the action applies to (`user`, `module`, `configuration_model`, `agent`, `scheduler_config`, `saml_config`, `error_groups`, `scheduler_task`). |
| `details`    | text          | Human-readable description of the event.          |
| `ip_address` | varchar(45)   | Source IP of the caller (nullable).               |
| `created_at` | datetime      | Event timestamp.                                  |

> **Before/after value.** The trail captures a free-text `details` message, not
> structured before/after columns. It therefore does **not** systematically
> record the old and new value of a changed field. The main exception is **tag
> changes** (`tag.set` / `tag.delete`): their `details` message records both the
> previous value(s) and the new value (for the tag key, e.g. `environment`),
> along with the actor and source IP. Other changes only describe the resulting
> state in prose.

---

## Audited events

| Event                                              | `action`                               | `target`             |
|----------------------------------------------------|----------------------------------------|----------------------|
| Configuration (MOF) upload                         | `create`                               | `configuration_model`|
| Configuration (MOF) deletion                       | `delete`                               | `configuration_model`|
| Configuration revert to a version                  | `revert`                               | `configuration_model`|
| Module upload (`.nupkg`)                           | `create`                               | `module`             |
| Module deletion                                    | `delete`                               | `module`             |
| User create / update / delete                      | `create` / `update` / `delete`         | `user`               |
| User password change                               | `update`                               | `user`               |
| Agent pre-enrollment (admin, with tags)             | `preenroll`                            | `agent`              |
| Agent tag set (single or bulk)                      | `tag.set`                              | `agent`              |
| Agent tag delete (single value or all)              | `tag.delete`                           | `agent`              |
| Agent trash / restore                              | `trash` / `restore`                    | `agent`              |
| Bulk agent trash / restore                         | `trash` / `restore`                    | `agent`              |
| SAML configuration update                          | `update`                               | `saml_config`        |
| Error-group status update                          | `error_group.<status>`                 | `error_groups`       |
| Scheduler configuration update                     | `update`                               | `scheduler_config`   |
| Manual scheduler task run                          | `run`                                  | `scheduler_task`     |
| Successful user login                              | `login`                                | `users`              |

---

## How to query it

- **Web UI** — `GET /web/admin/audit` (`templates/audit.tmpl`), admin only.
- **API** (all admin only):
  - `GET /api/v1/audit` — paginated JSON, DataTables-compatible, with filters
    (`users`, `actions`, `date_from`, `date_to`, `search`) and ordering.
  - `GET /api/v1/audit/filters` — distinct users and actions for dropdowns.
  - `GET /api/v1/audit/export` — full CSV export (`audit_export.csv`).
- **Direct SQL**:
  ```sql
  SELECT id, user_email, action, target, details, ip_address, created_at
  FROM audit
  ORDER BY created_at DESC
  LIMIT 100;
  ```

---

## Retention

The purge/cleanup workers **never remove audit entries**. A code review of every
cleanup path confirms there is no `DELETE FROM audit` (or truncate/drop of the
audit table) anywhere in the application:

- `report_cleanup` → deletes from `reports` and `error_occurrences`.
- `agent_purge` → deletes from `agents` and linked child rows.
- `configuration_version_cleanup` → prunes `configuration_model` versions.
- `log_rotation` → deletes rotated log files on disk.
- `error_occurrence` purge → deletes from `error_occurrences` only.

There is **no audit-specific retention setting**: audit rows are retained
indefinitely. Operators who need bounded retention or archival must implement
it externally (there is no built-in prune/archive mechanism for the audit
table).

---

## Access control

Only the **admin** role can read the audit trail. Every audit route is wrapped
in the `adminOnly` middleware, which checks `auth.IsAdmin` (`role == "admin"`):

- `GET /api/v1/audit`, `GET /api/v1/audit/filters`, `GET /api/v1/audit/export`
  → `jwtAuthMiddleware(adminOnly(...))`.
- `/web/admin/audit` → `WebJWTAuthMiddleware` + `WebAdminOnly`.

Regular `user`-role accounts cannot read audit entries via the API or the web
UI.

---

## Sensitive data

Audit entries and the log file do **not** contain secrets:

- A password change logs the target email and id — **never** the password.
- No audit entry or log line includes the registration key, the JWT
  `shared_secret`, or the contents of a MOF/module.
- The local log file and the syslog output sanitize control characters (to
  prevent log-line injection) and never emit the secret values.

---

## Review — gaps & follow-up issues

The feature was reviewed against the points in the issue. The gaps below were
identified; each maps to its own follow-up issue.

### 1. Failed logins are not audited
Only **successful** logins are recorded (`UpdateLastLogon` → `InsertAudit`).
Failed login attempts (unknown user, wrong password, SAML-source password
attempt) only increment the in-memory rate limiter and return `401` — no audit
entry. This is a security-monitoring gap.

### 2. DSC node registration is not audited
`RegisterHandler` (`PUT`/`POST /PSDSCPullServer.svc/{node}`) — the actual DSC
enrollment or certificate renewal by a node — writes log lines but never an
audit entry. Only the admin-initiated `preenroll` is audited.

### 3. MOF/module download by nodes is not audited
The DSC endpoints that serve artifacts to nodes — `ConfigurationContent` (MOF)
and `ModuleContent` (module) — do not write an audit entry. There is no record
of *who* pulled *what*.

### 4. Forced certificate rotation is not audited
`AgentRotateCertificateHandler` (`POST`/`DELETE /api/v1/agents/{id}/rotate-certificate`)
changes `agents.force_cert_rotation` without writing an audit entry.

### 5. ZIP module upload audit uses a stale schema (bug)
The `.zip` path in `module_upload.go` executes
`INSERT INTO audit (user, action, object, details, extra, timestamp)`, which
does **not** match the real `audit` table
(`user_email, action, target, details, ip_address, created_at`). The insert
fails and is silently ignored, so ZIP module uploads are effectively **not**
audited (the `.nupkg` path is correct).

### 6. Audit table is not created on SQLite (bug)
The `audit` table DDL exists only in `migration_v0.0.6_to_v1.0.0.sql`
(MSSQL syntax). Neither `db/schema_sqlite.sql` nor `db/schema_mssql.sql`
defines it. On a SQLite deployment the table is absent, so every `InsertAudit`
errors out and is swallowed (`_ = InsertAudit(...)`) — the audit trail is
effectively unavailable on SQLite.

### 7. No structured before/after value
The audit table has no `before`/`after` columns; only a free-text `details`
message is stored. A reviewer cannot systematically see the old vs. new value of
a changed field.

### 8. No integrity / tamper protection
The `audit` table is a plain writable table. A database admin (or a bug) can
`UPDATE` or `DELETE` audit rows; there is no append-only trigger, no immutable
log design, and no hash chaining to detect tampering.

### 9. No bulk structured export
There is a paginated JSON list and a CSV export, but no bulk, machine-readable
export endpoint that is easy to consume incrementally (e.g. a durable/offset
feed). The syslog output partially addresses SIEM forwarding via structured
data, but there is no native JSON export of the full audit trail.
