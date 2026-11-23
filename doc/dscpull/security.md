# DSC Security

This page describes how the DSC Pull Server authenticates clients and secures its endpoints. It covers the DSC node (LCM) authentication model, the client-certificate validation modes, the initial administrator account, and the recommended production setup.

---

## DSC Authentication Model

The DSC protocol (MS-DSCPM) does **not** authenticate requests with an application token (no `Bearer`/`Token` like the web API). Security relies on two distinct mechanisms:

1. **Registration** (`PUT /PSDSCPullServer.svc/{node}`): authenticated by the **registration key**. The node computes an HMAC-SHA256 signature of the request body (`x-ms-date` + hash) with this key. The server validates it in constant time. This key is configured in `dsc_pullserver.registrationKey`.

2. **Pull endpoints** (`GetDscAction`, `ConfigurationContent`, `SendReport`, `ModuleContent`): authenticated at the **transport layer** by a **client certificate** (mutual TLS). The server identifies the node by the **certificate thumbprint** and verifies it against the enrolled node in the `agents` table. No HTTP token is involved — adding one would break the DSC protocol.

### Per-node certificate binding

In addition to the global "known thumbprint" check, the pull endpoints **bind the presented certificate to the `agentId` in the request path** (`verifyNodeCertBinding`):

- `GetDscAction`, `SendReport` and `ConfigurationContent` reject the request (**HTTP 403**) when the certificate thumbprint presented by the client does **not** match the `certificate_thumbprint` stored for the `agentId` in the path. This stops a node from impersonating **another** enrolled agent using a certificate whose thumbprint is merely "known" to the server.
- `ConfigurationContent` additionally enforces that the requested configuration is **linked to that node** (an `agent_configurations` row for `agent_id` + `configuration_name`). Without this, a caller could download **any** MOF by name (configuration MOFs often embed secrets).
- The binding only applies when `enable_client_cert_validation` is `true`. When it is `false` (the default), the deployment has explicitly opted out of mTLS identity: the binding check is skipped (no behavioral change), but the `ConfigurationContent` **configuration-to-node linkage** check is still enforced so that configuration MOFs cannot be pulled by name by an arbitrary caller.

---

## Client Certificate Validation

Controlled in `config.json` under `dsc_pullserver`:

- **`enable_client_cert_validation`**: when `true`, the DSC endpoint **always** requires a client certificate. Recommended for production.
- **`bypass_ca_validation`**: only controls the **CA/chain validation** of the presented certificate, **not** whether a certificate is required. A client certificate is always required when `enable_client_cert_validation` is `true`:

| `enable_client_cert_validation` | `bypass_ca_validation` | `ClientAuth` | Behaviour |
|---|---|---|---|
| `true` | `true` | `RequireAnyClientCert` | Any certificate accepted at the TLS layer. The node is identified **only** by its enrolled thumbprint (self-signed node certificates work). Classic DSC model. |
| `true` | `false` | `RequireAndVerifyClientCert` | The certificate chain/signature is additionally validated against the **OS system trust store**, **and** the enrolled thumbprint is checked. |
| `false` | *any* | `NoClientCert` | No client certificate is requested (only for testing/isolated networks). |

> **No `client_ca_file` field is needed.** With `RequireAndVerifyClientCert` and no `ClientCAs` configured, Go validates the client chain against the OS system trust store.

### HTTPS is required

A client certificate requires HTTPS. In pure HTTP mode (`enable_https: false`) no certificate is negotiated, so the pull endpoints are unauthenticated at the transport level — only use HTTP for testing or isolated networks.

---

## Node Certificate Lifecycle & Renewal

At registration the node sends `RegistrationInformation.CertificateInformation` (thumbprint, subject, issuer, `notBefore`/`notAfter`), which the server stores in the `agents` table. The middleware uses the **certificate thumbprint** to identify the node on subsequent pull requests.

There are **two** ways to renew a node certificate, both supported:

1. **Server-initiated rotation (recommended, MS-DSCPM `CertificateRotation`)**. When the DB setting `certificate_rotation_enabled` is `true`, the server adds the `DSC-certificateRotation: True` response header to node-scoped pull responses (`GetDscAction`, `ConfigurationContent`, `SendReport`) when the node's certificate is inside the configured renewal window (`certificate_renewal_days` days before `certificate_notafter`, or already expired). The LCM then sends `POST /PSDSCPullServer.svc/Nodes(AgentId='…')/CertificateRotation` with its **new** `CertificateInformation` **wrapped in `RotationInformation`** (`RotationInformation.CertificateInformation`, the spec « RotateCertificate request » envelope); the server stores it and answers **HTTP 201**, clearing `force_cert_rotation`. `state`, `registered_at`, `last_communication`, `has_error_last_report` and `needs_recompile` are preserved.

2. **Client-driven re-registration (classic)**. The node re-registers on the same endpoint (`PUT`/`POST /PSDSCPullServer.svc/{node}`) with the same registration key. The server refreshes the stored certificate fields; the new thumbprint becomes the one checked by the middleware. `state`, `last_communication`, `has_error_last_report`, `needs_recompile` and `registered_at` are preserved. This works on both SQLite and MSSQL.

> **Windows LCM caveat (important).** On Windows, `CertificateRotation` is **not** supported on older releases (Product Behavior `<17>`: Windows 7 / Server 2008 R2 / 2012 / 8.1 / 2012 R2 / Windows 10). Where it **is** supported, the LCM still does **not** mint a fresh certificate by itself — it publishes the certificate it already has, so `CertificateRotation` mainly **syncs the node's current thumbprint to the server**. A real rollover is driven by the certificate lifecycle on the node (auto-enrolment / GPO / manual install) and is completed on the server by **re-registration** (`Set-DscLocalConfigurationManager -Path <LCMConfig>`). Treat `CertificateRotation` as "sync the thumbprint", not as a self-service renewal.

### When does rotation trigger?

The MS-DSCPM protocol has **no protocol-level timer** that forces a renewal: `DSC-certificateRotation` (§2.2.2.8) is a **`MAY`**, i.e. the server decides. In this implementation the server emits the header **opportunistically** on the next pull request of a node, based on the **database setting** `dsc_settings`:

| Setting | Default | Purpose |
|---|---|---|
| `certificate_rotation_enabled` | `0` (off) | Enables emitting `DSC-certificateRotation: True` to nodes whose certificate is inside the renewal window. **Opt-in**; when off the server behaves as before (classic re-registration only). |
| `certificate_renewal_days` | `30` | Renewal window: number of days before `certificate_notafter` at which the server starts asking the node to rotate. A **zero/negative** value disables the time-based trigger even if the feature is enabled. |

The renewal is therefore **"when the node's certificate expires within N days"**, evaluated from the stored `agents.certificate_notafter` (sent by the node at registration or during a previous rotation). The window is **adjustable at runtime** through the admin API (`GET`/`PUT /api/v1/dsc/settings`, admin-only) and is stored in the `dsc_settings` table — not in `config.json`. Use the `db/migration_v1.6.7.sql` script (MSSQL) or the schema files to create the table on existing databases.

**Admin-forced renewal (ASAP).** An administrator can request that a **specific** node rotate its certificate on its **next** cycle, regardless of the renewal window and even when `certificate_rotation_enabled` is off. This sets `agents.force_cert_rotation` to `1`; `maybeSetCertificateRotationHeader` then emits `DSC-certificateRotation: True` unconditionally on that node's next pull. The flag is cleared automatically once the rotation succeeds (the `CertificateRotation` handler) or on re-registration. The admin API is `POST` / `DELETE /api/v1/agents/{id}/rotate-certificate` (admin-only), and the corresponding button is on the node detail page.

### CertificateRotation security model

The rotation endpoint authenticates with the node's **current (old) certificate** at the TLS layer: `verifyNodeCertBinding` requires the presented thumbprint to equal the one stored for the `agentId` in the path. The **new** `CertificateInformation` only travels in the request body. Consequently, an attacker cannot rotate a node's certificate unless they already hold the node's **current** private key — there is no hijack vector (you must already be the legitimate node to replace its certificate).

> **Note on the middleware:** the DSC client-certificate middleware (main.go) exempts only the registration route (`PUT`) from the "known thumbprint" check. `CertificateRotation` is **not** exempt: the presented certificate must be a thumbprint already registered in the `agents` table. Since the client presents its current (registered) certificate, the request passes both the middleware and the per-node binding.

### When does the header NOT appear?

- When `certificate_rotation_enabled` is `false` (default) — **unless the node is force-flagged** (`agents.force_cert_rotation = 1`, i.e. an admin asked for an immediate renewal).
- When the node has no `certificate_notafter` stored.
- When the certificate expires more than `certificate_renewal_days` days from now — **unless the node is force-flagged** (an admin override solicits the rotation regardless of the window).
- On the `CertificateRotation` and registration responses themselves (the spec explicitly excludes them).
- On `ModuleContent`: this endpoint has no node identifier in its URL (`GET /PSDSCPullServer.svc/{module}/ModuleContent`), so the header is not emitted there even though the client certificate is present.

---

## Initial Administrator Account

The server **never creates a default admin account** (the old `admin@localhost / password` is gone). To create the first administrator, run the binary in CLI mode **once**, locally:

```
DSC-Admin-Console.exe init-bootstrap-admin
```

- Reads `config.json`, initializes the DB schema if needed (requires the `db/` directory next to the executable).
- Creates `admin@localhost` (role `admin`, source `local`) **only if no user exists**.
- Generates a **cryptographically secure 30-character password** and prints it **once**.
- **Refuses** to run if any user already exists (no account overwrite).
- **Refuses to run when stdout is not a terminal.** The initial password is printed in clear text once; to avoid leaking it into a redirected file or a CI log, the command aborts unless stdout is a terminal (mutual `isatty` check). Do **not** run it with output redirected (`> file`) or in an automation pipeline.
- The server logs a `WARN` pointing to this command when no user is present.

Keep the printed password safe and change it after the first login. Clear the terminal scrollback after reading it.

---

## Startup Security Warnings

The server validates a few security-sensitive settings at startup and logs a `WARN` when a protection is likely inactive or missing:

| Condition | WARN |
|---|---|
| DSC endpoint served over plain HTTP (`enable_https: false`) | `DSC Server is listening in HTTP (no TLS)` |
| `enable_client_cert_validation: true` **and** DSC served over HTTP | No client certificate is negotiated, so the pull endpoints are **not** protected by mutual TLS. |
| Web UI / API served over plain HTTP (`web_ui.enable_https: false`) | Credentials and JWT tokens transit unencrypted. |
| No `shared_secret` (neither `web_ui.shared_secret` nor `dsc_pullserver.shared_secret`) | Web/API authentication will fail until a value is set. |
| `dsc_pullserver.registrationKey` empty | DSC nodes will not be able to register. |

Treat these warnings as errors in production. The defaults in the shipped `config.json` examples enable HTTPS; if you disable it, make sure you only do so on an isolated network.

---

## Login Brute-Force Protection

The `/api/v1/login` endpoint is protected by an **in-memory, per-client-IP rate limiter** (no external dependency):

- After **5 failed attempts** within a **15-minute window**, the client IP is temporarily blocked.
- Blocked attempts return **HTTP 429** with `Retry-After: 900` and `X-RateLimit-Limit: 5`.
- A **successful** login clears the failure counter for that IP.
- The window is rolling: after the oldest failure ages out, the IP can try again.

> **Caveat:** the limiter is per-process and in-memory. Restarting the server resets the counters, and for a multi-instance deployment the limiter is per node.

> **Reverse proxy:** the limiter keys on the client IP. When the server is fronted by a reverse proxy, the `X-Forwarded-For` / `X-Real-IP` headers are only honored if the direct peer (`RemoteAddr`) is listed in `web_ui.trusted_proxies` (a list of IPs/CIDRs). If you do not configure `trusted_proxies`, the limiter uses the direct peer and all clients behind a proxy are collapsed into the proxy's IP. Configure it (e.g. `"trusted_proxies": ["10.0.0.0/8"]`) so each real client is tracked separately. Header spoofing from an untrusted peer is ignored, preventing brute-force bypass.

> **Request body limits:** DSC endpoints (registration, `SendReport`, `GetDscAction`) and uploads (`modules/upload`, configuration MOF, SAML SP key/cert) are bounded server-side (`20 MiB` and `64 MiB` respectively) to prevent memory-exhaustion attacks.

---

## Security Notes & Recommendations

- Always use **HTTPS** in production (both the DSC endpoint and the web UI).
- Enable `enable_client_cert_validation` and, when node certificates can be issued by a trusted authority, set `bypass_ca_validation: false`.
- Treat the **registration key** (`registrationKey`) as a secret and rotate it carefully (re-registration is required on change).
- Restrict admin access and review the logs (`logs/` directory) regularly.
- Rotate the SAML SP key/cert and the JWT `shared_secret` periodically.
- Keep the database connection and schema up to date (see the [Database Schema](../DatabaseSchema.md) reference).
