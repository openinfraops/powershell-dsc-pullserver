# Release v1.8.1

Date: 2026-11-26

Patch release fixing the `/metrics` authentication so a single user API token can be used to scrape both `/api/v1/*` and `/metrics`, and adding two console UI conveniences (a dedicated error-groups page and the pending-reboot state on the node page).

## API changes

- *No changes under `/api/v1/`.*
- *Additive (outside `/api/v1/`):* `GET /metrics` now also accepts a valid user API token presented as `Authorization: Token <token>` — the same token accepted by the `/api/v1/*` endpoints — in addition to the existing `Bearer <metrics_token>` header and `?token=<token>` query parameter. This lets Zabbix / Prometheus scrape `/metrics` with the same `{$TOKEN_API}` used for the REST API. The endpoint no longer returns `404` when `web_ui.metrics_token` is unset; it is always exposed and now returns `401` when no valid credential is presented.

  As before, `/health` and `/metrics` live outside `/api/v1/`, so this does not affect the provider-facing contract.

## Changes

### `/metrics` accepts a user API token

Previously `/metrics` was gated solely by `web_ui.metrics_token` and, when that key was unset, the endpoint returned `404` (not exposed). Operators therefore had to configure a dedicated `metrics_token` to scrape metrics — a second credential on top of the user API token used for `/api/v1/*`. Now `MetricsHandler` accepts either the configured `web_ui.metrics_token` (`Bearer <metrics_token>` / `?token=<token>`) or a valid user API token (`Authorization: Token <token>`), validated against the same store as the REST API. The empty-token `404` short-circuit was removed, so the endpoint always answers `200` (valid credential) or `401` (no/valid credential).

This is the change the Zabbix template relies on: a single `{$TOKEN_API}` now drives both `/api/v1/*` and `/metrics`.

### Console: dedicated error-groups page

Adds a `GET /web/errors` page (and `handlers/web_error_groups.go`, `templates/error_groups.tmpl`, `web/error_groups.js`) that surfaces DSC report error groups in the console, backed by the `/api/v1/error_groups` endpoints introduced in v1.8.0.

### Console: pending-reboot state on the node page

The node detail page (`web/node.js`, `templates/node.tmpl`) now displays an agent's pending-reboot state (`reboot_pending` / `reboot_loop`) surfaced by `/api/v1/agents/{id}`.

## Impacted files

- `handlers/metrics_api.go`, `handlers/monitoring_test.go`
- `handlers/web_error_groups.go`
- `internal/routes/web_routes.go`, `internal/schema/appconfig.go`
- `templates/error_groups.tmpl`, `templates/menu.tmpl`, `templates/node.tmpl`
- `web/error_groups.js`, `web/node.js`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `build/version.json`
- `.github/workflows/release.yml`
- `doc/release_v1.8.1.md`
