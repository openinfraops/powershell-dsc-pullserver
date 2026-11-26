# Release v1.8.2

Date: 2026-11-26

Patch release fixing an MSSQL (T-SQL) compatibility bug that broke the DSC error-grouping endpoints and page.

## API changes

**No API changes.** The `/api/v1/error_groups*` endpoints keep the same contract; this release only stops them from returning `500` ("DB query error") on Microsoft SQL Server.

## Changes

### Error-grouping queries made T-SQL-aware

The error-grouping feature (`GET /api/v1/error_groups`, `GET /api/v1/error_groups/{fingerprint}/nodes`, and the report-ingest `ErrorGroupsListHandler`) used SQLite-style paging (`LIMIT ? OFFSET ?`) and `ORDER BY ... LIMIT 1` without a T-SQL branch. `LIMIT` is not valid in T-SQL, so on Microsoft SQL Server the endpoint failed with a generic `500` "DB query error" (surfaced in the console as **« Erreur de chargement : DB query error »** on the DSC errors page).

The queries now follow the convention used everywhere else in the codebase:

- `ListErrorGroups` / `ListErrorGroupNodes` emit `OFFSET ? ROWS FETCH NEXT ? ROWS ONLY` on MSSQL, `LIMIT ? OFFSET ?` on SQLite.
- `currentActiveConfigVersionTx` / `LatestActiveConfig` use `SELECT TOP 1` on MSSQL instead of `ORDER BY ... LIMIT 1`.

The driver is read from `global.AppConfig.Database.Driver` (helper `isMSSQL()`), so no call sites change and SQLite behaviour is unchanged.

## Impacted files

- `internal/db/error_groups.go`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `build/version.json`
- `.github/workflows/release.yml`
- `doc/release_v1.8.2.md`
