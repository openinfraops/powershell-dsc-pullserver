# MS-DSCPM Protocol Implementation Status

This page records which operations of the **Microsoft Desired State Configuration Pull Model Protocol ([MS-DSCPM])** are implemented by `go-dsc-pull`, and how. The reference specification in the repository is `doc/[MS-DSCPM].pdf` (v20240423).

The **DSC Pull Server Protocol** lets a DSC node (LCM) register with a pull server, pull configurations and modules, report its status, and (since this release) query its own reports and rotate its client certificate. All DSC operations are versioned; this server implements the **Version 2.0** wire format for the operations it supports.

> **Reading the matrix:** ✅ = fully implemented; ⚠️ = partially implemented (only the 2.0 path, or a documented deviation); ❌ = not implemented.

---

## Operations matrix

| Operation | Version | HTTP request | MS-DSCPM section (v20240423) | Status |
|---|---|---|---|---|
| **RegisterDscAgent** | 2.0 | `PUT` / `POST` `/PSDSCPullServer.svc/{node}` (§3.9) | §3.9 | ✅ |
| **GetDscAction** | 2.0 | `POST` `/PSDSCPullServer.svc/{id}/GetDscAction` (§3.8) | §3.8 | ✅ |
| **GetConfiguration** | 2.0 | `GET` `/PSDSCPullServer.svc/{node}/{config}/ConfigurationContent` (§3.6) | §3.6 | ✅ |
| **GetConfiguration** | 1.0 / 1.1 | `GET` `/PSDSCPullServer.svc/{node}/{config}/ConfigurationContent` (§3.2) | §3.2 | ⚠️ Served the same as 2.0 (the 2.0 request/response is produced regardless of the client's declared version) |
| **GetModule** | 2.0 | `GET` `/PSDSCPullServer.svc/{module}/ModuleContent` (§3.7) | §3.7 | ✅ |
| **GetModule** | 1.0 / 1.1 | `GET` `/PSDSCPullServer.svc/{module}/ModuleContent` | §3.? (legacy) | ❌ |
| **SendReport** | 2.0 | `POST` `/PSDSCPullServer.svc/{node}/SendReport` (§3.10) | §3.10 | ✅ |
| **GetReports** | 2.0 | `GET` `/PSDSCPullServer.svc/{node}/Reports(JobId=…)` (§3.11) | §3.11 | ✅ *(added in this release)* |
| **CertificateRotation** | 2.0 | `POST` `/PSDSCPullServer.svc/{node}/CertificateRotation` (§3.12) | §3.12 | ✅ *(added in this release)* |
| **SendStatusReport** | 1.0 / 1.1 | `POST` `/PSDSCPullServer.svc/{node}/SendStatusReport` (§3.4) | §3.4 | ❌ |
| **GetStatusReport** | 1.0 / 1.1 | `GET` `/PSDSCPullServer.svc/{node}/StatusReport` (§3.5) | §3.5 | ❌ |

Every implemented operation returns the **`ProtocolVersion: 2.0`** response header (where applicable) and responds with the status codes required by the spec (e.g. `200`, `400`, `401`, `404`, `409`).

---

## Message headers

| Header | Section | Status |
|---|---|---|
| `ProtocolVersion` (response) | §2.2.2.5 | ✅ emitted on DSC responses |
| `AgentId` (request) | §2.2.2.6 | ✅ parsed from the request path (and, where present, used for cert binding) |
| `Authorization` (request, `Shared`) | §2.2.2.7 | ✅ validated for registration (HMAC-SHA256 of the body with the registration key) |
| `Content-Type` | §2.2.2.1.2 | ✅ `application/json` |
| `DSC-certificateRotation` (response) | §2.2.2.8 | ✅ emitted to nodes inside the renewal window (see below) |

---

## Common URI parameters

| Parameter | Section | Status |
|---|---|---|
| `ConfigurationId` | §2.2.3.1 | ⚠️ Accepted in the query string and mapped to the configuration name where relevant |
| `ModuleName` | §2.2.3.2 | ✅ used for module downloads |
| `ModuleVersion` | §2.2.3.3 | ✅ used for module downloads |
| `Checksum` | §2.2.3.4 | ✅ verified for module downloads |

---

## GetReports (GET) — §3.11

**Added in this release.** The client retrieves one stored report by `JobId`:

```
GET /PSDSCPullServer.svc/Nodes(AgentId='<agentId>')/Reports(JobId='<jobId>')
```

- **Request:** no body (§3.11.5.1.1.1).
- **Response:** the **full report JSON** exactly as the client originally sent it via `SendReport` (the server stores the raw body in `reports.raw_json`). `Content-Type: application/json`, `ProtocolVersion: 2.0`. Status `200 OK`.
- **Authentication:** the node is bound to the `agentId` in the path (`verifyNodeCertBinding`) — the presented client certificate must match the stored thumbprint. Status codes `400`, `401`, `403`, `404` are returned as appropriate.

> **Deviation from the spec:** §3.11.5.1.1.2 describes the response as `ReportContent` (a blob). This server returns the stored `raw_json` byte-for-byte, which is the report `ReportContent` as sent by the client.

---

## CertificateRotation (POST) — §3.12

**Added in this release.** The client proactively rotates its certificate by publishing its **new** certificate. Per the MS-DSCPM "RotateCertificate request" schema (§3.12.5.1.1.1), the new certificate is wrapped under `RotationInformation`:

```
POST /PSDSCPullServer.svc/Nodes(AgentId='<agentId>')/CertificateRotation
Content-Type: application/json

{
  "RotationInformation": {
    "CertificateInformation": {
      "Thumbprint": "<new-thumbprint>",
      "Subject": "<new-subject>",
      "Issuer": "<new-issuer>",
      "NotBefore": "<new-notbefore>",
      "NotAfter": "<new-notafter>",
      "FriendlyName": "<friendly-name>",
      "PublicKey": "<base64>",
      "Version": 3
    }
  }
}
```

> **Compatibility:** the handler reads `RotationInformation.CertificateInformation` first (the spec-correct envelope), and falls back to a flat top-level `CertificateInformation` for backward compatibility with older clients. A missing `CertificateInformation` — in either location — returns `400 Bad Request`.

- **Response:** no body (§3.12.5.1.1.2). Status **`201 Created`** on success.
- **Status codes:** `201` created · `400` malformed/missing `CertificateInformation`/`Thumbprint` · `401` unauthorized certificate · `403` cert-to-node binding failed · `404` unknown node/job · `409` rotation not enabled (spec allows the server to reject a rotation it did not solicit).
- **State preserved:** only `certificate_thumbprint`, `certificate_subject`, `certificate_issuer`, `certificate_notbefore`, `certificate_notafter`, `certificate_public_key`, `certificate_key_algorithm` and `force_cert_rotation` (reset to `0`) are updated; `state`, `registered_at`, `last_communication`, `has_error_last_report`, `needs_recompile` and `deleted_at` are left untouched.
- **Public key:** the `CertificateInformation.PublicKey` field (§3.12) is read and normalized to a PEM `PUBLIC KEY` plus a key-algorithm label (`RSA`/`ECDSA`/`Ed25519`), stored in `certificate_public_key` / `certificate_key_algorithm`. **Observed wire format (Windows LCM):** the field is `<base64>;<OID-algorithm>` — e.g. `MIIBCgKCAQEA…QIDAQAB;1.2.840.113549.1.1.1` (`1.2.840.113549.1.1.1` = rsaEncryption), where the base64 part is a **PKCS#1 `RSAPublicKey`** (`SEQUENCE { INTEGER modulus, INTEGER exponent }`), not a PKIX `SubjectPublicKeyInfo`. The parser therefore tries `x509.ParsePKIXPublicKey` (EC/Ed25519) then `x509.ParsePKCS1PublicKey` (RSA), on the base64 part, ignoring the trailing `;OID`. If the field is absent or not parseable, the previous value is kept (the next node re-registration refreshes it from the presented certificate). These columns are **not consumed today** — reserved for future per-node secret encryption.

### Soliciting a rotation (DSC-certificateRotation header)

Per §2.2.2.8, the header `DSC-certificateRotation: True` is a **`MAY`** and the server decides when to send it. This server emits it **when the node's certificate is inside the DB-configured renewal window**:

- The **database setting** `dsc_settings.certificate_rotation_enabled` (default `0`, **opt-in**) turns the feature on.
- `dsc_settings.certificate_renewal_days` (default `30`) defines the window: the server starts soliciting rotation when the node's `certificate_notafter` is within N days (or already expired).
- An **admin can force** a specific node to rotate on its next cycle (the "renew now" button on the node page, or the API `POST` / `DELETE /api/v1/agents/{id}/rotate-certificate`). This raises `agents.force_cert_rotation`, making the server emit the header **regardless** of the window and of the global enable flag. The flag is cleared automatically once the rotation succeeds.
- The header is added to node-scoped responses (`GetDscAction`, `ConfigurationContent`, `SendReport`). It is **not** emitted on `CertificateRotation`, registration, or `ModuleContent` responses (the latter has no node identifier in its URL).

See [DSC Security](security.md) for the "when" details and the security model of the rotation endpoint. The settings are adjustable at runtime through the admin API `GET`/`PUT /api/v1/dsc/settings`.

### End-to-end renewal flow

1. **Decision.** On each node-scoped pull, `maybeSetCertificateRotationHeader` decides whether to solicit a rotation. It reads `agents.force_cert_rotation` and `agents.certificate_notafter`, then emits the header when either:
   - the node is **force-flagged** (`force_cert_rotation = 1`, admin override), or
   - `certificate_rotation_enabled` is on **and** `certificate_notafter` is within `certificate_renewal_days` days (or already expired).
2. **Solicitation.** The header `DSC-certificateRotation: True` is added to the response (`GetDscAction`, `ConfigurationContent`, `SendReport`).
3. **Client action.** The LCM, seeing the header, sends `POST .../CertificateRotation` with its **current** certificate wrapped in `RotationInformation.CertificateInformation`.
4. **Store.** The server updates the `agents` certificate columns and **clears** `force_cert_rotation`, answers `201 Created`.

> **Windows LCM caveat.** On Windows, `CertificateRotation` is **not** available on older releases (Product Behavior `<17>`: Windows 7 / Server 2008 R2 / 2012 / 8.1 / 2012 R2 / Win10). And even where it is supported, the LCM does **not** mint a fresh certificate by itself — it publishes the certificate it already has. A real rollover is therefore driven by the certificate lifecycle on the node (auto-enrolment / GPO / manual install), and the **re-registration** `Set-DscLocalConfigurationManager -Path <LCMConfig>` completes the handshake. `CertificateRotation` is best understood as "sync the node's current thumbprint to the server" rather than a self-service renewal.

### Observability

Every rotation decision and result is written to **both** sinks:
- the standard logger (`log.Printf`, the process `stdout`/`stderr`), and
- the log file via `logs.WriteLogFile` (`dsc-pull-server.log`).

Lines are prefixed `[CERTROTATION]`, e.g.:

```
[CERTROTATION] Soliciting certificate rotation for agentId="946D6360-...": forced by admin (force_cert_rotation=1)
[CERTROTATION] Rotated certificate for agentId="946D6360-...": new thumbprint=<sha1>
[CERTROTATION] Failed to read agent "..." for rotation decision: <err>
```

---

## Not implemented (known gaps)

- **`SendStatusReport` / `GetStatusReport` (v1.0 / 1.1):** these legacy report endpoints are not exposed. The LMS should use `SendReport` / `GetReports` (v2.0).
- **`GetModule` (v1.0 / 1.1):** only the v2.0 module format is served.
- **Version negotiation:** the server always answers with the v2.0 wire format; a client that only speaks v1.x `GetConfiguration`/`GetModule` is served the v2.0 response.

---

## Product behavior notes

The following MS-DSCPM **Product Behavior** notes apply to this server's supported matrix:

- `<5>` (§2.2.2.8) — `DSC-certificateRotation` is a server `MAY`: implemented, controlled by the DB setting.
- `<16>` (§3.11) — `GetReports` is not available on Windows Server 2008 R2 / 2012 / 2012 R2 (LCM limitation, not server-side).
- `<17>` (§3.12) — `CertificateRotation` is not available on Windows 7 / Server 2008 R2 / 2012 / 8.1 / 2012 R2 / Windows 10 (LCM limitation, not server-side).

---
*Last updated for release v1.7.2.*
