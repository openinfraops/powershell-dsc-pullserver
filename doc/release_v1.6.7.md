# Release v1.6.7

Date: 2026-11-23

Release adding two MS-DSCPM protocol gaps and DB-backed certificate renewal.

## Changes

### DB-backed certificate renewal configuration

The DSC certificate-renewal behaviour is now configured in the database (`dsc_settings` table, single row `id=1`) rather than in `config.json`:

| Setting | Default | Purpose |
|---|---|---|
| `certificate_rotation_enabled` | `0` (off) | Enables emitting the `DSC-certificateRotation: True` response header to nodes whose certificate is inside the renewal window. **Opt-in.** |
| `certificate_renewal_days` | `30` | Renewal window in days before certificate expiration at which the server solicits rotation. |

- Adjustable at runtime via the admin API `GET` / `PUT /api/v1/dsc/settings` (admin-only), no service restart required.
- The `dsc_settings` table is created automatically by the boot-time schema (`schema_sqlite.sql` / `schema_mssql.sql`); for existing MSSQL deployments a manual `db/migration_v1.6.7.sql` is provided.

### Certificate rotation (`CertificateRotation`, MS-DSCPM §3.12)

- New endpoint `POST /PSDSCPullServer.svc/Nodes(AgentId='…')/CertificateRotation`.
- The node publishes its **new** `CertificateInformation`; the server stores it and answers **HTTP 201** (no body).
- The server solicits rotation by emitting `DSC-certificateRotation: True` (§2.2.2.8) on node-scoped pull responses (`GetDscAction`, `ConfigurationContent`, `SendReport`) when the node's certificate is within the DB-configured renewal window.
- **Security:** the endpoint authenticates with the node's **current** certificate at the TLS layer (`verifyNodeCertBinding`); the new certificate travels in the request body. There is no hijack vector — you must already hold the current private key to rotate.
- `state`, `registered_at`, `last_communication`, `has_error_last_report` and `needs_recompile` are preserved; only the certificate fields are refreshed.

### `GetReports` (MS-DSCPM §3.11)

- New endpoint `GET /PSDSCPullServer.svc/Nodes(AgentId='…')/Reports(JobId='…')`.
- Returns the stored report (`raw_json`) as sent by the client via `SendReport`, `Content-Type: application/json`, `ProtocolVersion: 2.0`.
- Bound to the node via the presented client certificate.

### Documentation

- New [MS-DSCPM protocol implementation state](doc/dscpull/protocol-implementation.md) page (referenced from the root `README.md` and the DSC-pull docs index).
- [DSC Security](doc/dscpull/security.md) documents the certificate-renewal "when", the DB settings, and the rotation security model.
- [DSC Pull Server configuration](doc/dscpull/configuration.md) documents the DB-stored settings.

## Impacted files

- `db/schema_sqlite.sql`, `db/schema_mssql.sql`, `db/migration_v1.6.7.sql`
- `internal/db/dsc_settings.go`, `internal/db/initdb.go`
- `handlers/certificate_rotation.go`, `handlers/getreports.go`, `handlers/dsc_settings_api.go`, `handlers/sendreport.go`, `handlers/configuration_content.go`, `handlers/getdscaction_node.go`
- `internal/routes/dsc_routes.go`, `internal/routes/web_routes.go`
- `doc/dscpull/protocol-implementation.md`, `doc/dscpull/security.md`, `doc/dscpull/configuration.md`, `doc/dscpull/README.md`, `README.md`
