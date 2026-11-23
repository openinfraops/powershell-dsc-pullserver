# Storage Modes

This page describes how the DSC Pull Server stores the artifacts it manages — resource **modules** and **configuration MOFs** — and the two storage modes available.

## Database storage (primary)

All payloads are stored as **BLOBs** in the database, giving a single, transactional, centrally-backed store:

- **Resource modules** are stored in the `modules` table (`zip_blob`). See [Resource Modules](modules.md) for upload/management.
- **Configuration MOFs** are stored in the `configuration_model` table (`mof_file`), versioned via `original_name` / `previous_id`. See [MOF Configurations](configurations.md).

Because everything lives in the database, backups are one operation and there is no filesystem/DB consistency to reconcile.

### Database engine

The engine is chosen in `config.json` → `database.driver`:

- **`sqlite`** (embedded). The DB file (`database.name`, e.g. `dsc.db`) is created **next to the executable** (a relative path is resolved against the executable directory, `db.go` / `main.go`). The `db/` install directory contains **only the read-only SQL schema files** (`schema_sqlite.sql`), not the data file.
- **`mssql`** (or `sqlserver`). Connection via `server` / `port` / `user` / `password` / `name`. **Timestamps are stored in UTC** (`GETUTCDATE()`).

The schema is applied automatically at startup (`InitDB` reads `db/schema_sqlite.sql` or `db/schema_mssql.sql`, which are idempotent — `CREATE TABLE IF NOT EXISTS` on SQLite, `IF NOT EXISTS`/`ALTER TABLE` guards on MSSQL).

See [Database Schema](../DatabaseSchema.md) for the full table reference.

## Configuration repository (Git-backed)

When the configuration-repository feature is enabled, the **source** configurations (`.psd1`, etc.) live in a **Git repository** — the source of truth for configuration content:

1. The server checks the repository out to `configuration_repo_settings.checkout_directory` (default `data/configuration-repository`, resolved relative to the executable if not absolute).
2. It runs the configured compiler script (`compiler_script`, default `scripts/Compile-Configurations.ps1`) with `pwsh` to produce one `.mof` per configuration.
3. The compiled MOFs are imported into the `configuration_model` table (BLOB).

So the Git repository holds the **source**, while the database remains the **serving store** that nodes pull from. On SQLite the DB file and the checkout are two separate things — back both up.

These settings are stored in the database (`configuration_repo_settings`), managed from the **Scheduler** administration page, and are **not** edited in `config.json` (the `configuration_repo_*` keys are only a legacy migration source imported once).

See [Configuration repository (Git) & Scheduler](../dscpull/configuration-repo.md) for the full settings reference and the compiler contract.

## Migration

- Schema upgrades are applied via **`db/migration_*.sql`** scripts (see the `db/` directory). Apply the relevant script for the target version, replace the executable, then restart.
- The current schema version is tracked in the `dsc_infra_info` table (`db_version`).
- Back up the database (and the configuration-repository checkout if used) before any upgrade.

> **Note:** `storage.md` is the storage/architecture overview. For the web UI workflows see [Resource Modules](modules.md) and [MOF Configurations](configurations.md); for the underlying tables and columns see [Database Schema](../DatabaseSchema.md).
