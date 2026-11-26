# Release v1.8.3

Date: 2026-11-26

Patch release neutralising the noise caused by generic report-level error
messages (e.g. `Unknown`) when a DSC node fails without a usable message.

## API changes

**No API changes.** The `/api/v1/error_groups*` endpoints keep the same
contract; this release only stops the console from creating empty
`unknown` error groups for placeholder messages.

## Changes

### Ignore generic report-level error messages in classification

When a DSC node fails without a usable message (e.g. Event 4131
`DscTimerConsistencyOperationResult` with `ErrorCode=1` and
`ErrorMessage=NULL`), the client emits a bare placeholder such as `Unknown`
in the report-level `Errors` array. Previously this created a noisy group
(message `Unknown`, category `unknown`, no resource type) that cluttered the
DSC errors console.

`ExtractReportErrorParts` now skips generic placeholder messages in both the
report-level `Errors` array and the per-resource failed `StatusData` entries
(previously only empty/malformed entries were skipped). A new helper
`isGenericErrorMessage()` plus the `genericErrorMessages` / `genericErrorWords`
sets recognise bare placeholders (`Unknown`, `NULL`, `Error`, `n/a`, `-`, …)
and messages composed only of generic tokens (`error null`, `unknown unknown`,
…).

Informative messages (`Report-level failure`, `Access is denied`,
`Resource failed`, `ok`, `good`, exit codes, HRESULTs) are still kept, so the
existing default category rules are unaffected.

> Note: this affects **new** reports. An `Unknown` group already stored in the
> database is not automatically removed; it can be marked `resolved` via
> `PUT /api/v1/error_groups/{fingerprint}` (or it will age out).

## Impacted files

- `internal/schema/errorclassify.go`
- `internal/schema/errorclassify_test.go`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `build/version.json`
- `.github/workflows/release.yml`
- `doc/release_v1.8.3.md`
