# Release v1.2.11

Date: 2026-11-22

Release introducing a shared, process-wide database connection pool and a clean Windows service shutdown, plus two security hardening fixes on the DSC pull endpoints and the `configuration_models` API.

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

## Behavior notes

- With `enable_client_cert_validation: false` (the default), the certificate-to-node binding is a no-op, so existing HTTP/no-mTLS deployments are unaffected. The `ConfigurationContent` configuration-to-node **linkage** check, however, is always enforced.
- A node whose configuration is **not** linked via `agent_configurations` can no longer pull that MOF through `ConfigurationContent`. If a deployment relied on pulling a configuration **without** a linking row, it must add the `agent_configurations` entry (the DSC register flow and the `GetDscAction` fallback create it automatically for registered nodes).

## Tests

- Existing handler/auth/db/service tests continue to pass. `go build ./...` and `go vet ./...` are clean.
