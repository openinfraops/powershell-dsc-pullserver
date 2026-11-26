# DSC Pull Server Configuration

This page describes the expected structure and options of the DSC Pull Server configuration file (`config.json`). The exhaustive reference for **every** section (`database`, `dsc_pullserver`, `web_ui`, `saml`) is in [Configuration.md](../Configuration.md); this page focuses on the `dsc_pullserver` block and the mutual-TLS settings.

## Main Section: `dsc_pullserver`

Example:

```json
{
  "dsc_pullserver": {
    "cert_file": "dsc-dev.local.crt",
    "enable_https": true,
    "enable_client_cert_validation": true,
    "bypass_ca_validation": true,
    "key_file": "dsc-dev.local.key",
    "port": 8484,
    "registrationKey": "AnyString",
    "shared_secret": "pQw8vZ3rT6sJkL2xQ9eBfG7hN1uVtX4yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1"
  }
}
```

### Option Descriptions

| Key                              | Description |
|----------------------------------|-------------|
| **cert_file**                    | Path to the server's SSL certificate file. Required for HTTPS. |
| **key_file**                     | Path to the server's private key file. Required for HTTPS. |
| **enable_https**                 | Set to `true` to enable HTTPS. If `false`, the server runs in plain HTTP mode. **The server emits a `WARN` at startup when running over plain HTTP.** In HTTP mode no client certificate is negotiated, so mutual TLS is unavailable. **If `enable_client_cert_validation` is also `true`, the server refuses to start (fail-closed):** it logs an `ERROR [SECURITY]` naming both settings and exits with a non-zero code, rather than running with an mTLS protection that is not actually active. |
| **enable_client_cert_validation** | If `true`, the DSC endpoint **always** requires a client certificate (**mutual TLS**). Recommended for production. `false` disables mutual TLS entirely. |
| **bypass_ca_validation**         | Controls the **CA/chain validation** of the presented client certificate, **not** whether a certificate is required. A client certificate is always required when `enable_client_cert_validation` is `true`, whatever this value: |
|                                  | - `true`: the server accepts any certificate without validating its signature/chain (suitable for **self-signed** node certificates). The node is identified **only** by its enrolled certificate thumbprint. This is the classic DSC model. |
|                                  | - `false`: the server additionally validates the certificate chain/signature against the **OS system trust store** (`RequireAndVerifyClientCert`), **and** still checks the enrolled thumbprint. |
| **port**                         | Port the server listens on (default: `8484`). |
| **registrationKey**              | Key required for agent registration. Set any string value; agents must provide it to register. **The server emits a `WARN` at startup if this value is empty** (no node would be able to register). |

> **Generating secure secrets:** use [scripts/Generate-SecureKeys.ps1](../../scripts/Generate-SecureKeys.ps1) to generate a cryptographically strong `shared_secret` and `registrationKey`. **The server also emits a `WARN [SECURITY]` at startup if either value is too short, is a known placeholder, or has too little character diversity.**

### Optional / operational keys

These keys are also accepted in the `dsc_pullserver` block (shared `HTTPConfig` struct). They are documented in [Configuration.md](../Configuration.md) and can be left at their default in a minimal setup:

- **shared_secret**: JWT signing secret for the web/API. `web_ui.shared_secret` takes precedence when both are set. Example (replace with your own strong value): `"pQw8vZ3rT6sJkL2xQ9eBfG7hN1uVtX4yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1"`.
- **enable_report_auto_cleanup**, **report_retention_days**, **report_cleanup_interval_mins**: periodic cleanup of old reports.
- **enable_release_check**, **release_check_interval_mins**: periodic GitHub latest-release check.
- **enable_log_rotation**, **log_rotate_max_size_mb**, **log_rotate_max_backups**, **log_rotate_max_age_days**: log file rotation.
- **enable_agent_purge**, **agent_purge_retention_days**, **agent_purge_interval_mins**: periodic purge of soft-deleted agents.
- **enable_configuration_version_cleanup**, **configuration_version_max_keep**, **configuration_version_cleanup_interval_mins**: retention of old configuration versions.
- **enable_configuration_repo_sync**, **configuration_repo_***: legacy migration source for the Git-backed configuration repository. These are **imported once** and then **stored in the database** (`configuration_repo_settings`), managed from the **Scheduler administration page** — see [Configuration repository (Git) & Scheduler](configuration-repo.md).

## Settings stored in the database (not `config.json`)

Some DSC behaviour is **not** configured in `config.json` but in the **`dsc_settings` table** (single row, `id=1`). These are adjustable at runtime via the admin API (`GET`/`PUT /api/v1/dsc/settings`) without restarting the service:

| Setting | Default | Purpose |
|---|---|---|
| `certificate_rotation_enabled` | `0` (`false`) | Enables server-initiated certificate rotation (`DSC-certificateRotation: True` header). **Opt-in.** |
| `certificate_renewal_days` | `30` | Renewal window in days before certificate expiration at which the server solicits rotation. |

In addition, a **per-node** override exists regardless of these settings: an admin can request an ASAP renewal for a specific node from the node page (or `POST` / `DELETE /api/v1/agents/{id}/rotate-certificate`), which sets `agents.force_cert_rotation`. See [Security](security.md).

### Reading / setting the renewal window

`certificate_renewal_days` is the **renewal window**, not the certificate's lifetime: it is the number of days **before** `certificate_notafter` at which the server starts soliciting a rotation. The certificate's actual validity period is chosen by the certificate authority (auto-enrolment / GPO / manual request) and is **not** configured server-side. Example:

```bash
# Read current settings
curl -k -H "Authorization: Bearer <jwt>" https://dsc-console:443/api/v1/dsc/settings
# {"certificate_rotation_enabled":true,"certificate_renewal_days":30}

# Enable rotation and widen the window to 45 days
curl -k -X PUT -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"certificate_rotation_enabled":true,"certificate_renewal_days":45}' \
  https://dsc-console:443/api/v1/dsc/settings
```

> There is **no admin UI page** for these two settings yet; they are configured through the admin API `GET`/`PUT /api/v1/dsc/settings` (admin-only, `jwtAuthMiddleware + adminOnly`). The per-node "renew now" button (which sets `force_cert_rotation`) is on the node detail page.

See [Security](security.md) for the certificate-renewal "when", and [MS-DSCPM protocol implementation state](protocol-implementation.md) for the `CertificateRotation` / `GetReports` endpoints.

## Additional Sections

The `database`, `web_ui`, and `saml` blocks are described in [Configuration.md](../Configuration.md). The `web_ui` block shares the same structure as `dsc_pullserver` (both use `HTTPConfig`); in practice only `cert_file`, `key_file`, `enable_https`, `port`, `shared_secret`, `trusted_proxies` and `metrics_token` are meaningful for the web/API server.

## Monitoring endpoints

The web/API server exposes two monitoring endpoints:

### `GET /health`

Unauthenticated (for load balancers, Zabbix and container health checks). Returns JSON:

```json
{
  "status": "ok",
  "version": "1.7.2",
  "db": { "reachable": true },
  "workers": { "alive": true, "running_tasks": 0 }
}
```

- `status` is `ok`, `degraded` (database unreachable) or `unavailable` (process shutting down).
- `workers.alive` is `false` when the process is shutting down **or** the database is unreachable (single-process model).
- `workers.running_tasks` is the count of scheduler tasks currently in `running` state.
- No node names or secrets are exposed.

### `GET /metrics`

Prometheus text format, **protected** by `web_ui.metrics_token`:

- **Empty token (default):** the endpoint is **not exposed** — any request returns `404`.
- **Token set:** the caller must present it via `Authorization: Bearer <token>` or `?token=<token>`; a missing/wrong token returns `401`.

The endpoint reports the build version, database/network health, node counts by compliance state, the stalest node last-report age, the last/next run of each scheduler task, the `401`/`403` HTTP response counters, the web TLS certificate expiry and release-update state. **It exposes no node names or secrets** (only build version, compliance states, scheduler task keys and task statuses).

```bash
# Scrape with a dedicated token (set web_ui.metrics_token in config.json)
curl -H "Authorization: Bearer <token>" https://dsc-console:443/metrics
curl --url "https://dsc-console:443/metrics?token=<token>"
```

> **Production:** always set a strong, random `metrics_token` (generate one with [scripts/Generate-SecureKeys.ps1](../../scripts/Generate-SecureKeys.ps1)) and serve the web UI over HTTPS. Do **not** leave `metrics_token` empty on a publicly reachable server — the endpoint is only safe to publish when the surrounding network already restricts access.

## Related pages

- [DSC Pull Server documentation](README.md)
- [Node registration](registration.md)
- [Security](security.md)
- [MS-DSCPM protocol implementation state](protocol-implementation.md)
- [Configuration repository (Git) & Scheduler](configuration-repo.md)
- [Full configuration reference](../Configuration.md)

---

> Ensure all paths are correct and certificates are valid for secure operation.
