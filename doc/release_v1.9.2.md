# Release v1.9.2

Date: 2026-11-27

Patch release fixing a MSSQL registration failure that left a node enrolled on
the pull server unable to register (the node stayed non-enrolled and every
`GetDscAction` / `ConfigurationContent` call returned `Certificate
unauthorized`).

## API changes

**No API changes.** The `/api/v1/*` endpoints keep the same contract. This
release only fixes the MSSQL agent-insert on the registration path.

## Changes

### Fix: MSSQL agent insert on registration (migration & upsert) failed

On MSSQL, the `migrateTempAgentToRealID` insert declared **13** parameter
placeholders (`1` for the `IF NOT EXISTS ... agent_id = ?` guard + `12` in the
`VALUES` list) for only **11** node/certificate columns — `registered_at`,
`last_communication`, `state` and `has_error_last_report` are SQL literals
(`GETUTCDATE()`, `GETUTCDATE()`, `'pending_apply'`, `0`) — against **12**
arguments passed. The insert therefore failed with:

```
[ERROR][REGISTER][DB] Migration of TEMP agent TEMP-... to <real-id> failed:
insert real agent <real-id>: sql: expected 13 arguments, got 12
```

Because the agent row was never created, the node stayed in
`waiting_for_registration` (pending enroll), had no `certificate_thumbprint`,
and the DSC middleware (`IsCertificateThumbprintRegistered`) rejected every
subsequent call with **`401 Certificate unauthorized`** — the node could never
complete enrollment on MSSQL.

The extra placeholder was removed so the `VALUES` list matches the 11 columns
and the 12 arguments (the `realAgentID` is used once for the guard and once for
the `agent_id` column).

The `isNewNode` branch of `upsertAgent` used by the same registration flow was
also aligned: it previously omitted the `state` column (leaving the node in an
undefined `NULL` state instead of the intended `pending_apply`) and passed
`NULL` for `registered_at`/`last_communication`, overriding their schema
`DEFAULT GETUTCDATE()`. It now uses the same column/literal layout.

## Impacted files

- `handlers/register.go`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `build/version.json`
- `.github/workflows/release.yml`
- `doc/dscpull/configuration.md`
- `SECURITY.md`, `doc/Integrations.md`, `doc/README.md`
- `doc/release_v1.9.2.md`
