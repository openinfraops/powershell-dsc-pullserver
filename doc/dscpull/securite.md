# DSC Security

This page describes how the DSC Pull Server authenticates clients and secures its endpoints. It covers the DSC node (LCM) authentication model, the client-certificate validation modes, the initial administrator account, and the recommended production setup.

---

## DSC Authentication Model

The DSC protocol (MS-DSCPM) does **not** authenticate requests with an application token (no `Bearer`/`Token` like the web API). Security relies on two distinct mechanisms:

1. **Registration** (`PUT /PSDSCPullServer.svc/{node}`): authenticated by the **registration key**. The node computes an HMAC-SHA256 signature of the request body (`x-ms-date` + hash) with this key. The server validates it in constant time. This key is configured in `dsc_pullserver.registrationKey`.

2. **Pull endpoints** (`GetDscAction`, `ConfigurationContent`, `SendReport`, `ModuleContent`): authenticated at the **transport layer** by a **client certificate** (mutual TLS). The server identifies the node by the **certificate thumbprint** and verifies it against the enrolled node in the `agents` table. No HTTP token is involved — adding one would break the DSC protocol.

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

## Security Notes & Recommendations

- Always use **HTTPS** in production (both the DSC endpoint and the web UI).
- Enable `enable_client_cert_validation` and, when node certificates can be issued by a trusted authority, set `bypass_ca_validation: false`.
- Treat the **registration key** (`registrationKey`) as a secret and rotate it carefully (re-registration is required on change).
- Restrict admin access and review the logs (`logs/` directory) regularly.
- Rotate the SAML SP key/cert and the JWT `shared_secret` periodically.
- Keep the database connection and schema up to date (see the [Database Schema](../DatabaseSchema.md) reference).
