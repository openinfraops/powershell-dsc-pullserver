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

When a node's certificate is renewed, the node **re-registers** (same endpoint, same registration key). The server refreshes the stored certificate fields; the new thumbprint becomes the one checked by the middleware. `state`, `last_communication`, `has_error_last_report`, `needs_recompile` and `registered_at` are preserved. This works on both SQLite and MSSQL.

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
- The server logs a `WARN` pointing to this command when no user is present.

Keep the printed password safe and change it after the first login.

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
