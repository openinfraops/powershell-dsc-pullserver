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

### Documentation

- [MS-DSCPM protocol implementation state](doc/dscpull/protocol-implementation.md) documents the spec-correct `RotationInformation` payload, the end-to-end renewal flow, the Windows LCM caveat, and the `[CERTROTATION]` observability.
- [DSC Security](doc/dscpull/security.md) updated with the `RotationInformation` envelope and the Windows LCM caveat.
- [DSC Pull Server configuration](doc/dscpull/configuration.md) documents how to read/set the renewal window via the admin API.
- [API v1 reference](doc/APIv1.md) adds the `GET`/`PUT /api/v1/dsc/settings` endpoints.
- [Configuration repository](doc/dscpull/configuration-repo.md) notes the lazy node-context resolution.

## Impacted files

- `handlers/certificate_rotation.go`, `handlers/certificate_rotation_test.go`, `handlers/agent_rotate_cert_api_test.go`
- `main.go`
- `internal/service/configuration_repo.go`
- `doc/dscpull/protocol-implementation.md`, `doc/dscpull/security.md`, `doc/dscpull/configuration.md`, `doc/dscpull/configuration-repo.md`, `doc/APIv1.md`
