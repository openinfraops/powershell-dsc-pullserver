# Database Schema Reference

This page documents the database schema for the DSC Pull Server. The same data model is supported on both backends (SQLite and MSSQL); the canonical definitions are the SQL files below. Use this reference to understand the data model — for the authoritative DDL always refer to the schema files.

## Supported Databases & Schema Files

| Backend | Schema file | Notes |
|---------|-------------|-------|
| SQLite  | `db/schema_sqlite.sql` | Uses `TEXT`/`INTEGER`/`TIMESTAMP`/`BLOB`, `AUTOINCREMENT`, `CURRENT_TIMESTAMP`. |
| MSSQL   | `db/schema_mssql.sql`  | Uses `NVARCHAR`/`INT IDENTITY`/`BIT`/`DATETIME`/`VARBINARY(MAX)`, `GETDATE()`. |

> Type mapping (SQLite → MSSQL): `TEXT`→`NVARCHAR`, `INTEGER`→`INT`, `BOOLEAN`→`BIT`, `TIMESTAMP`→`DATETIME`, `BLOB`→`VARBINARY(MAX)`, auto-increment `INTEGER PRIMARY KEY AUTOINCREMENT`→`INT IDENTITY(1,1) PRIMARY KEY`.

---

## Tables

### `users`
User accounts for the web/API authentication.

| Column           | Type    | Description                          |
|------------------|---------|--------------------------------------|
| id               | INTEGER | PK, auto-increment                  |
| first_name       | TEXT    | First name                          |
| last_name        | TEXT    | Last name                           |
| email            | TEXT    | Unique email (login)                |
| password_hash    | TEXT    | Bcrypt password hash                |
| is_active        | BOOLEAN | Active flag (default 1)             |
| created_at       | TIMESTAMP | Creation timestamp (default now)  |
| last_logon_date  | TIMESTAMP | Last successful login (nullable)  |
| role             | TEXT    | `admin` or `user` (default `user`)  |
| source           | TEXT    | `local` or `saml` (default `local`) |

### `user_api_tokens`
API tokens used for programmatic access (stored as a hash, not plaintext).

| Column       | Type    | Description                          |
|--------------|---------|--------------------------------------|
| id           | INTEGER | PK, auto-increment                  |
| user_id      | INTEGER | FK → `users.id`                     |
| token_hash   | TEXT    | Hash of the API token               |
| label        | TEXT    | Human-readable label (nullable)     |
| is_active    | BOOLEAN | Active flag (default 1)             |
| created_at   | TIMESTAMP | Creation timestamp (default now)  |
| revoked_at   | TIMESTAMP | Revocation timestamp (nullable)   |

### `agents`
DSC nodes (agents). Also stores the node certificate used for client-cert authentication.

| Column                    | Type    | Description                              |
|---------------------------|---------|------------------------------------------|
| agent_id                  | TEXT    | PK, agent/registration identifier (GUID) |
| node_name                 | TEXT    | Node name                                |
| lcm_version               | TEXT    | LCM version                              |
| registration_type         | TEXT    | Registration message type                |
| certificate_thumbprint    | TEXT    | SHA-1 thumbprint of the node cert        |
| certificate_subject       | TEXT    | Certificate subject                      |
| certificate_issuer        | TEXT    | Certificate issuer                       |
| certificate_notbefore     | TEXT    | Certificate validity start (raw)         |
| certificate_notafter      | TEXT    | Certificate validity end (raw)           |
| certificate_public_key    | TEXT    | Node cert public key, PEM `PUBLIC KEY` (nullable). Captured from the presented mTLS client cert at registration, and from the `CertificateInformation.PublicKey` field on `CertificateRotation`. Not consumed yet — reserved for per-node secret encryption. |
| certificate_key_algorithm | TEXT    | Node cert public-key algorithm: `RSA`/`ECDSA`/`Ed25519` (nullable). `RSA` supports direct encryption; `ECDSA` would require key agreement (ECDH). |
| registered_at             | TIMESTAMP | Registration timestamp (default now)  |
| last_communication        | TIMESTAMP | Last check-in timestamp (default now) |
| has_error_last_report     | BOOLEAN | Whether the last report had errors       |
| needs_recompile           | BOOLEAN | Whether the node needs recompilation     |
| force_cert_rotation       | BOOLEAN | Admin-requested ASAP certificate renewal (default 0); cleared on successful rotation |
| deleted_at                | TIMESTAMP | Soft-delete timestamp (nullable)      |
| deleted_by                | TEXT    | Who soft-deleted the agent (nullable)    |
| state                     | TEXT    | `success`/`failure`/`pending_apply`/...  |

Indexes (MSSQL): `idx_agents_state` on `state`.

### `agent_tags`
Key/value tags attached to an agent.

| Column     | Type    | Description             |
|------------|---------|--------------------------|
| agent_id   | TEXT    | FK → `agents.agent_id`  |
| tag_key    | TEXT    | Tag key                 |
| tag_value  | TEXT    | Tag value               |

PK = (`agent_id`, `tag_key`, `tag_value`).

### `agent_configurations`
Which configuration names are assigned to an agent.

| Column             | Type | Description            |
|--------------------|------|-------------------------|
| agent_id           | TEXT | FK → `agents.agent_id` |
| configuration_name | TEXT | Configuration name     |

PK = (`agent_id`, `configuration_name`). Index (MSSQL): `idx_agent_configurations_configuration_name` on `configuration_name`.

### `agent_ips`
IP addresses observed for an agent.

| Column     | Type | Description            |
|------------|------|-------------------------|
| agent_id   | TEXT | FK → `agents.agent_id` |
| ip_address | TEXT | IP address             |

PK = (`agent_id`, `ip_address`).

### `configuration_model`
Versioned MOF configuration files.

| Column         | Type      | Description                                  |
|----------------|-----------|----------------------------------------------|
| id             | INTEGER   | PK, auto-increment                          |
| name           | TEXT      | Configuration name                          |
| original_name  | TEXT      | Original name when versioned (nullable)     |
| previous_id    | INTEGER   | FK → `configuration_model.id` (nullable)    |
| upload_date    | TIMESTAMP | Upload timestamp (default now)              |
| uploaded_by    | TEXT      | User who uploaded (not null)                |
| mof_file       | BLOB      | MOF content                                 |
| last_usage     | TIMESTAMP | Last time the MOF was applied (nullable)    |

Indexes (MSSQL): `idx_configuration_model_name` on `name`, `idx_configuration_model_original_name` on `original_name`.

### `reports`
DSC compliance reports sent by nodes.

| Column                | Type     | Description                       |
|-----------------------|----------|-----------------------------------|
| id                    | INTEGER  | PK, auto-increment               |
| agent_id              | TEXT     | Agent that sent the report       |
| job_id                | TEXT     | DSC job identifier               |
| report_format_version | TEXT     | Report format version            |
| operation_type        | TEXT     | Operation type                   |
| refresh_mode          | TEXT     | Refresh mode                     |
| status                | TEXT     | Report status                    |
| start_time            | TEXT     | Operation start time             |
| end_time              | TEXT     | Operation end time               |
| reboot_requested      | TEXT     | Whether a reboot was requested   |
| errors                | TEXT     | JSON array of errors             |
| status_data           | TEXT     | JSON array of status data        |
| additional_data       | TEXT     | JSON array of additional data    |
| mof_applied           | BOOLEAN  | Whether a MOF was applied        |
| created_at            | TIMESTAMP | Report timestamp (default now) |
| raw_json              | TEXT     | Original raw report JSON         |

Indexes: `idx_reports_agent_id` on `agent_id`; (MSSQL) `idx_reports_job_id` on `job_id`.

### `modules`
Uploaded PowerShell resource modules.

| Column      | Type    | Description                 |
|-------------|---------|-----------------------------|
| id          | INTEGER | PK, auto-increment         |
| name        | TEXT    | Module name                |
| version     | TEXT    | Module version             |
| checksum    | TEXT    | Content checksum           |
| zip_blob    | BLOB    | Module package (zip)       |
| uploaded_at | TIMESTAMP | Upload timestamp (default now) |

Index (MSSQL): `idx_modules_name` on `name`.

### `properties`
Custom, user-defined properties.

| Column      | Type    | Description              |
|-------------|---------|--------------------------|
| id          | INTEGER | PK, auto-increment      |
| name        | TEXT    | Unique property name    |
| description | TEXT    | Description             |
| priority    | INTEGER | Priority (default 0)    |

### `node_properties`
Property values per node.

| Column      | Type    | Description                     |
|-------------|---------|---------------------------------|
| node_id     | TEXT    | Node identifier                 |
| property_id | INTEGER | FK → `properties.id`            |
| value       | TEXT    | Property value                  |

PK = (`node_id`, `property_id`).

---

## Infrastructure / Scheduler Tables

### `dsc_infra_info`
Single-row table holding app/infra metadata and release-check state.

| Column              | Type     | Description                        |
|---------------------|----------|------------------------------------|
| id                  | INTEGER  | PK, always `1` (`CHECK (id = 1)`) |
| web_version         | TEXT     | Web/API version                    |
| db_version          | TEXT     | DB schema version                  |
| latest_release      | TEXT     | Latest release tag (nullable)      |
| latest_release_url  | TEXT     | Release URL (nullable)             |
| update_available    | BOOLEAN  | Whether an update is available     |
| release_check_ok    | BOOLEAN  | Whether the last check succeeded   |
| release_checked_at  | TIMESTAMP | Last release check time (nullable) |
| updated_at          | TIMESTAMP | Last update timestamp              |

### `scheduler_tasks`
One row per scheduled background task.

| Column       | Type      | Description                        |
|--------------|-----------|------------------------------------|
| task_name    | TEXT      | PK, task key                       |
| display_name | TEXT      | Human-readable name                |
| next_run_at  | TIMESTAMP | Next scheduled run (nullable)      |
| last_run_at  | TIMESTAMP | Last run time (nullable)           |
| last_status  | TEXT      | `idle`/`running`/`success`/`failure` |
| last_message | TEXT      | Last run message (nullable)        |
| updated_at   | TIMESTAMP | Last update timestamp              |

### `scheduler_task_runs`
Execution history of scheduler tasks.

| Column         | Type      | Description                 |
|----------------|-----------|-----------------------------|
| id             | INTEGER   | PK, auto-increment         |
| task_name      | TEXT      | Task key                    |
| started_at     | TIMESTAMP | Run start time              |
| finished_at    | TIMESTAMP | Run end time (nullable)     |
| status         | TEXT      | Run status                  |
| message        | TEXT      | Run message (nullable)      |
| trigger_source | TEXT      | Manual/schedule trigger     |
| created_at     | TIMESTAMP | Creation timestamp          |

Index: `idx_scheduler_runs_task_started` on (`task_name`, `started_at DESC`).

### `configuration_repo_settings`
Single-row table holding the configuration-repository sync settings.

| Column                      | Type     | Description                       |
|-----------------------------|----------|-----------------------------------|
| id                          | INTEGER  | PK, always `1`                   |
| enabled                     | BOOLEAN  | Repository sync enabled          |
| repository_url              | TEXT     | Git repository URL               |
| branch                      | TEXT     | Branch to sync (default `main`)  |
| checkout_directory          | TEXT     | Local checkout directory         |
| compiler_script             | TEXT     | Compiler script path             |
| powershell_executable       | TEXT     | PowerShell executable (`pwsh`)   |
| repository_username         | TEXT     | Git username (optional)          |
| repository_password         | TEXT     | Git password/token (optional)    |
| sync_interval_mins          | INTEGER  | Sync interval in minutes         |
| compile_on_node_registration | BOOLEAN | Compile when a node registers   |
| legacy_imported             | BOOLEAN  | Whether legacy config was imported |
| updated_at                  | TIMESTAMP | Last update timestamp          |

---

## Relationships

- `user_api_tokens.user_id` → `users.id`
- `agents.certificate_*` — node certificate identity used by the client-cert middleware (enrollment thumbprint).
- `agent_tags.agent_id` → `agents.agent_id`
- `agent_configurations.agent_id` → `agents.agent_id`
- `agent_ips.agent_id` → `agents.agent_id`
- `configuration_model.previous_id` → `configuration_model.id` (version chain)
- `node_properties.property_id` → `properties.id`
- `reports.agent_id` → `agents.agent_id` (logical; no strict FK)

---

## Schema Evolution

- **SQLite**: update `db/schema_sqlite.sql`. Schema is created with `CREATE TABLE IF NOT EXISTS`; the server also calls `Ensure*Schema` helpers at startup to add columns missing on older databases.
- **MSSQL**: update `db/schema_mssql.sql`. MSSQL uses `IF NOT EXISTS` guards for idempotent creation and `ALTER TABLE ... ADD` migrations for new columns.

> Always back up your database before applying schema changes or upgrading.
