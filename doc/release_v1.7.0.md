# Release v1.7.0

Date: 2026-11-23

Release closing the MS-DSCPM protocol gaps (report retrieval and certificate rotation), adding an admin "renew certificate now" action, hardening the initial administrator bootstrap and the startup secrets, and bumping dependencies (Go 1.26).

## Changes

### MS-DSCPM protocol: `CertificateRotation` and `GetReports`

The protocol implementation now covers two previously-missing operations (see [MS-DSCPM protocol implementation state](doc/dscpull/protocol-implementation.md)):

- **`GetReports` (GET, §3.11)** — `GET /PSDSCPullServer.svc/Nodes(AgentId='…')/Reports(JobId='…')` returns the stored report (`reports.raw_json`) exactly as the client sent it via `SendReport`, `Content-Type: application/json`, `ProtocolVersion: 2.0`. Bound to the node via the presented client certificate.
- **`CertificateRotation` (POST, §3.12)** — `POST /PSDSCPullServer.svc/Nodes(AgentId='…')/CertificateRotation` lets the node publish its **new** `CertificateInformation`; the server stores it and answers **HTTP 201**. Authenticated with the node's **current** certificate at the TLS layer (`verifyNodeCertBinding`); the new certificate travels in the body. There is no hijack vector — you must already hold the current private key to rotate.

### DB-backed certificate renewal configuration

The renewal behaviour is now stored in the **`dsc_settings`** table (single row `id=1`) instead of `config.json`:

| Setting | Default | Purpose |
|---|---|---|
| `certificate_rotation_enabled` | `0` (off) | Enables emitting the `DSC-certificateRotation: True` response header to nodes whose certificate is inside the renewal window. **Opt-in.** |
| `certificate_renewal_days` | `30` | Renewal window in days before certificate expiration at which the server solicits rotation. |

- Adjustable at runtime via the admin API `GET` / `PUT /api/v1/dsc/settings` (admin-only), no service restart required.
- The server emits `DSC-certificateRotation: True` (§2.2.2.8) on node-scoped pull responses (`GetDscAction`, `ConfigurationContent`, `SendReport`) when the node's certificate is within the configured window (or already expired).

### Admin "renew certificate now" (ASAP)

- New admin-only API `POST` / `DELETE /api/v1/agents/{id}/rotate-certificate` raising/clearing the per-node `agents.force_cert_rotation` flag.
- New **"Renouveler le certificat"** button on the node detail page (admins only); it becomes "Renouvellement demandé (annuler)" while a request is pending.
- When `force_cert_rotation` is set, the server emits `DSC-certificateRotation: True` on the node's next pull **regardless** of the renewal window and even when `certificate_rotation_enabled` is off (the explicit admin request takes precedence).
- The flag is cleared automatically when the rotation succeeds (`CertificateRotation` handler) or on re-registration.

### Security hardening

- **Bootstrap admin:** `init-bootstrap-admin` now **refuses to run unless stdout is a terminal** (`isatty`). The generated initial password is printed in clear text **once** (it is stored as a bcrypt hash); refusing a redirected/CI output prevents the secret leaking into a file or a build log.
- **Secure secrets generation:** new [scripts/Generate-SecureKeys.ps1](scripts/Generate-SecureKeys.ps1) generates strong `shared_secret` / `registrationKey` values, and the server emits a `WARN [SECURITY]` at startup if a configured secret is too short, a known placeholder, or has too little character diversity.
- Removed a leftover debug response header (`X-Debug-Client-Body`) that echoed the request body.

### Windows service / installation naming

- The Windows service is now registered under the production name **`DSC-Console`** (and the bootstrap CLI under **`DSC-Admin-Console`**); scripts and documentation were aligned.

### Documentation

- New [MS-DSCPM protocol implementation state](doc/dscpull/protocol-implementation.md) page (referenced from the root `README.md` and the DSC-pull docs index).
- [DSC Security](doc/dscpull/security.md) documents the certificate-renewal "when", the DB settings, the rotation security model, and the bootstrap-admin guard.
- [DSC Pull Server configuration](doc/dscpull/configuration.md) documents the DB-stored settings.
- [Web UI storage modes](doc/webui/storage.md) expanded/corrected; installation guide fixes for log and DB file locations; configuration reference cross-links clarified.

### Dependencies (Go 1.26)

- `github.com/crewjam/saml` `0.5.1` → `0.6.3`
- `golang.org/x/crypto` `0.53.0` → `0.59.0`
- `github.com/go-git/go-git/v5` `5.19.2` → `5.19.3`
- `golang.org/x/sys` `0.46.0` → `0.50.0`
- The `go` directive is bumped to `1.26.0` and the CI build now uses Go `1.26.6` (see `.github/workflows/release.yml`).

## Impacted files

- `db/schema_sqlite.sql`, `db/schema_mssql.sql`, `db/migration_v1.6.7.sql`
- `internal/db/dsc_settings.go`, `internal/db/initdb.go`, `internal/db/agent_schema_ensure.go`
- `handlers/certificate_rotation.go`, `handlers/getreports.go`, `handlers/dsc_settings_api.go`, `handlers/agent_rotate_cert_api.go`, `handlers/sendreport.go`, `handlers/configuration_content.go`, `handlers/getdscaction_node.go`, `handlers/agent_byid_api.go`, `handlers/about_api.go`
- `internal/routes/dsc_routes.go`, `internal/routes/web_routes.go`
- `internal/schema/agent.go`, `bootstrap_admin.go`
- `web/node.js`
- `scripts/Generate-SecureKeys.ps1`, `scripts/build-windows.ps1`, `scripts/install-windows.ps1`, `scripts/uninstall-windows.ps1`, `scripts/update-windows.ps1`
- `build/version.json`, `.github/workflows/release.yml`, `go.mod`, `go.sum`
- `doc/dscpull/protocol-implementation.md`, `doc/dscpull/security.md`, `doc/dscpull/configuration.md`, `doc/dscpull/README.md`, `doc/webui/storage.md`, `doc/installation.md`, `doc/Configuration.md`, `doc/APIv1.md`, `doc/DatabaseSchema.md`, `README.md`
