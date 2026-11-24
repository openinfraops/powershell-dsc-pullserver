# Release v1.7.1

Date: 2026-11-23

Follow-up release fixing the certificate-rotation flow (validated end-to-end against a real Windows LCM) and removing an unnecessary per-node scan in the configuration-repository worker every cycle.

## Changes

### Certificate rotation works end-to-end

The MS-DSCPM `CertificateRotation` flow (added in v1.7.0) is now validated against a real Windows LCM. Three fixes were required:

- **`CAST(force_cert_rotation AS INT)` on MSSQL.** `agents.force_cert_rotation` is a `BIT` on MSSQL (an `INTEGER` on SQLite). The rotation decision (`maybeSetCertificateRotationHeader`) and the `CertificateRotation` handler scanned it into a Go `int`; on MSSQL the driver returns a `bool`, so `Scan(&int)` **failed silently** and the header was never emitted even when an admin asked for the rotation. The admin API already scanned it as a `bool`, which is why it reported `force_cert_rotation=true` while the pull path ignored it. Both reads now `CAST(... AS INT)` (portable across MSSQL and SQLite).
- **`[CERTROTATION]` logged to stdout/stderr as well as the log file.** The message was only written through `logs.WriteLogFile`, so it was invisible when operators tail the process stdout/stderr (where the `[GETDSCACTION-NODE]` / `[SENDREPORT]` lines appear). It is now emitted on **both** sinks.
- **Read `RotationInformation.CertificateInformation` (spec-correct payload).** Per the MS-DSCPM "RotateCertificate request" schema (§3.12.5.1.1.1), the LCM wraps the new certificate under `RotationInformation` — the handler read a flat top-level `CertificateInformation` and answered **400 Bad Request** on the real client request. It now reads `RotationInformation.CertificateInformation` first, falling back to the flat form for backward compatibility.

> **Windows LCM caveat.** `CertificateRotation` is not available on older Windows releases (Product Behavior `<17>`) and, where it is supported, the LCM publishes the certificate it already has rather than minting a new one. `CertificateRotation` is therefore best understood as "sync the node's current thumbprint to the server"; a real rollover is driven by the node certificate lifecycle and completed by re-registration (`Set-DscLocalConfigurationManager -Path <LCMConfig>`).

### Configuration-repository worker: no per-node scan on no-op syncs

The periodic worker resolved every node's compilation properties twice per cycle even when the repository had not changed (the common case). The per-node resolution is now **lazy** — it runs only when the repository actually changed and a compilation is about to run. An up-to-date repository triggers **no** `Node properties resolved` pass.

### Node certificate public key captured (building block)

The server now captures each node certificate's **public key** and **key algorithm** into the `agents` table (`certificate_public_key`, `certificate_key_algorithm`, both nullable):

- **Registration / re-registration** reads the public key from the **presented mTLS client certificate** (`r.TLS.PeerCertificates[0].PublicKey`).
- **`CertificateRotation`** reads it from the body's `CertificateInformation.PublicKey` (the **new** certificate). If absent/unparseable, the previous value is kept (the next node re-registration refreshes it).
- The key is normalized to a PEM `PUBLIC KEY` with an algorithm label (`RSA`/`ECDSA`/`Ed25519`).

**Not consumed yet:** these columns are reserved as a building block for a future **per-node secret-encryption** feature — the private key of a node's mTLS certificate lives on the node, so the server can encrypt a secret with the node's public key and the node can decrypt it. `RSA` supports direct encryption; `ECDSA` would require key agreement (ECDH). Schema added via `db/schema_*.sql` and `db/migration_v1.7.1.sql`.

### Documentation

- [MS-DSCPM protocol implementation state](dscpull/protocol-implementation.md) documents the spec-correct `RotationInformation` payload, the end-to-end renewal flow, the Windows LCM caveat, the `[CERTROTATION]` observability, and the public-key capture.
- [DSC Security](dscpull/security.md) updated with the `RotationInformation` envelope, the Windows LCM caveat, and the public-key capture (registration from the presented cert, rotation from the body).
- [DSC Pull Server configuration](dscpull/configuration.md) documents how to read/set the renewal window via the admin API.
- [API v1 reference](APIv1.md) adds the `GET`/`PUT /api/v1/dsc/settings` endpoints, and the agent detail endpoint now returns `certificate_public_key` / `certificate_key_algorithm`.
- [Configuration repository](dscpull/configuration-repo.md) notes the lazy node-context resolution.
- [Database schema](DatabaseSchema.md) documents the two new `agents` columns.

## Impacted files

- `handlers/certificate_rotation.go`, `handlers/certificate_rotation_test.go`, `handlers/agent_rotate_cert_api_test.go`
- `handlers/dsc_node_auth.go`, `handlers/dsc_node_auth_test.go`
- `handlers/register.go`, `handlers/register_test.go`
- `handlers/agent_byid_api.go`
- `internal/schema/agent.go`, `internal/db/agent_schema_ensure.go`
- `main.go`
- `internal/service/configuration_repo.go`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`, `db/migration_v1.7.1.sql`
- `doc/dscpull/protocol-implementation.md`, `doc/dscpull/security.md`, `doc/dscpull/configuration.md`, `doc/dscpull/configuration-repo.md`, `doc/APIv1.md`, `doc/DatabaseSchema.md`
