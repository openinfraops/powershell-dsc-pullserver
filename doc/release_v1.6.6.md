# Release v1.6.6

Date: 2026-11-22

Release bringing two hardening rounds: the shared, process-wide database connection pool with a clean Windows service shutdown, admin-only `configuration_models` and per-node certificate binding on the DSC pull endpoints, plus a review/hardening round covering server timeouts, index-friendly certificate-thumbprint lookups, English-only runtime logs, a scheduled-task abstraction and documentation corrections.

## Changes

### Shared database connection pool + graceful service shutdown

- The application now exposes a **single, process-wide `*sql.DB` pool** (`internal/db` → `SetShared`/`Shared`) reused by all handlers instead of each handler opening (and leaking) its own connection. Handlers must **never close** the shared pool.
- The Windows service lifecycle now uses a **cancellable context** (`internal/service/service.go`): `StartWindowsService`/`Execute` propagate a `context.Context` that is cancelled on shutdown, and the service waits on a `<-done` channel so it stops cleanly instead of being torn down mid-request.

Impacted files:

- internal/db/db.go
- internal/service/service.go
- main.go

### Admin-only `configuration_models` create/delete

- `POST /api/v1/configuration_models` and `DELETE /api/v1/configuration_models` now require the **admin** role (`auth.IsAdmin`); a non-admin user receives **HTTP 403**. This closes an authorization gap where any authenticated user could create or delete configuration models that are served to DSC nodes (configuration poisoning). It now matches `PUT` (update) and the module upload path, which were already admin-only.

Impacted files:

- handlers/configuration_model_api.go

### Per-node certificate binding on the DSC pull endpoints

- Added `verifyNodeCertBinding` (handlers/dsc_node_auth.go): when `enable_client_cert_validation` is `true`, the pull endpoints **bind the presented client certificate thumbprint to the `agentId` in the request path** and reject the request (**HTTP 403**) on mismatch. This stops one node from impersonating another enrolled agent.
- `ConfigurationContent` also enforces that the requested configuration is **linked to the requesting node** (`agent_configurations`), preventing the download of **any** MOF **by name** (configuration MOFs often embed secrets). This linkage check applies regardless of `enable_client_cert_validation`.

Impacted files:

- handlers/dsc_node_auth.go (new)
- handlers/configuration_content.go
- handlers/sendreport.go
- handlers/getdscaction_node.go

### Server timeouts against slowloris

- Both `http.Server`s (the DSC pull endpoints and the Web/API/UI mux) now set `ReadHeaderTimeout: 10s`, `ReadTimeout: 120s`, `WriteTimeout: 180s` and `IdleTimeout: 120s` to mitigate slowloris and slow-body resource exhaustion.

Impacted files:

- main.go

### Index-friendly certificate-thumbprint check

- The direct certificate-thumbprint SQL is moved out of `main.go` into `db.IsCertificateThumbprintRegistered`; the comparison is done **without `LOWER()` on the column**, keeping it index-friendly.
- The thumbprint is stored in **lowercase** at registration (`register.go`), and `LOWER()` is removed from `dsc_node_auth.go`.
- For pre-existing nodes (thumbprints stored in uppercase before this change), `verifyNodeCertBinding` compares with `strings.EqualFold`, and a one-time startup migration (`db.NormalizeCertificateThumbprints`) lowercases stored thumbprints (a harmless no-op on MSSQL's case-insensitive collation).

Impacted files:

- internal/db/cert.go (new)
- internal/db/cert_test.go (new)
- handlers/dsc_node_auth.go
- handlers/register.go
- main.go

### Scheduled-task abstraction

- `runApp` started six near-identical background workers (report cleanup, release check, log rotation, agent trash purge, config version retention, config repo sync). Each repeated the schedule computation and the begin/complete scheduler-task recording inline.
- A `taskMeta` type (`main_tasks.go`) now drives every worker, removing ~250 duplicated lines from `runApp` and making the scheduler display names consistent in English.

Impacted files:

- main_tasks.go (new)
- main.go

### English-only runtime logs

- Every runtime log emission (`WriteLogFile`, `log.Print*`, and CLI console output) is translated from French to English: server lifecycle, DSC TLS middleware, scheduled-task workers, the bootstrap-admin CLI, and the DSC/API handlers.
- HTTP error response bodies (`http.Error`) are intentionally left unchanged: they are part of the API contract consumed by the web UI, not log output.

### CLI alias cleanup & documentation correction

- The duplicate `bootstrap-admin` CLI alias is removed; only `init-bootstrap-admin` is kept.
- `doc/installation.md` now references the actual binary name `DSC-Admin-Console.exe` consistently (the release binary is built as such), instead of the incorrect `dsc-pull-server.exe`.

## Behavior notes

- With `enable_client_cert_validation: false` (the default), the certificate-to-node binding is a no-op, so existing HTTP/no-mTLS deployments are unaffected. The `ConfigurationContent` configuration-to-node **linkage** check, however, is always enforced.
- A node whose configuration is **not** linked via `agent_configurations` can no longer pull that MOF through `ConfigurationContent`. If a deployment relied on pulling a configuration **without** a linking row, it must add the `agent_configurations` entry (the DSC register flow and the `GetDscAction` fallback create it automatically for registered nodes).
- Certificate thumbprints are normalised to lowercase on startup for nodes registered before the change, so existing uppercase thumbprints remain recognised when client-certificate validation is enabled.

## Tests

- Existing handler/auth/db/service tests continue to pass. `go build ./...` and `go vet ./...` are clean. New `internal/db` tests cover thumbprint normalisation, idempotency and post-normalisation lookup.
