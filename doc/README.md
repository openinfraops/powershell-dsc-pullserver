# Documentation

The documentation is organized into three layers:

1. **Section guides** — operation-oriented pages grouped under the DSC Pull Server and Web UI indexes.
2. **Reference pages** — exhaustive, per-key/per-endpoint references and the database schema.
3. **Release notes** — per-version change summaries.

---

## Section indexes

- [DSC Pull Server](dscpull/README.md) — operation, installation, security, protocol implementation state, configuration repository, registration.
- [Web UI](webui/README.md) — authentication, resource modules, MOF configurations, permissions, storage modes.

## Reference pages

- [Installation guide](installation.md) — prerequisites, step-by-step install (Windows service), troubleshooting, upgrade.
- [Configuration reference](Configuration.md) — exhaustive per-key reference of every section (`database`, `dsc_pullserver`, `web_ui`, `saml`, `syslog`) with example files.
- [Forwarding logs to a syslog server](syslog.md) — optional RFC 5424 syslog output (UDP/TCP/TCP+TLS) with rsyslog and Zabbix examples.
- [Audit trail](Audit.md) — audited events, stored fields, retention, access control, and a review of gaps.
- [Database schema](DatabaseSchema.md) — supported databases, schema files, every table, relationships, schema evolution.
- [API v1 reference](APIv1.md) — the REST endpoints under `/api/v1/` (auth, users, agents, DSC settings, audit, modules, properties, configurations, SAML).
- [Integrations](Integrations.md) — Terraform provider, API compatibility matrix, API versioning & breaking-changes policy, and minimal API-token role.
- [SAML integration](SAML.md) — SAML single-sign-on configuration.

## Protocol reference (spec)

- [MS-DSCPM] Desired State Configuration Pull Model Protocol: [`[MS-DSCPM].pdf`](%5BMS-DSCPM%5D.pdf) (reference specification, v20240423).

## Tested platforms & support lifecycle

The pull server is validated **end-to-end** (registration, pull, report, certificate rotation) against real Windows LCMs running on the following Windows Server versions. Support dates are the Microsoft lifecycle end-of-support dates.

| Platform | Tested | Mainstream support end | Extended support end |
| --- | --- | --- | --- |
| Windows Server 2019 | Yes | Jan 9, 2024 | **Jan 9, 2029** |
| Windows Server 2022 | Yes | Oct 13, 2026 | **Oct 14, 2031** |

Dates per the Microsoft lifecycle pages for [Windows Server 2019](https://learn.microsoft.com/en-us/lifecycle/products/windows-server-2019) and [Windows Server 2022](https://learn.microsoft.com/en-us/lifecycle/products/windows-server-2022). After the **Extended support end** date a platform no longer receives security updates.

> **Lifecycle projection.** Because the solution is exercised against these two platforms, its longest supported horizon is bounded by the most restrictive of them — **Windows Server 2019, until January 9, 2029**. Plan the migration of the pull server and the LCM nodes to Windows Server 2022 (supported until **October 14, 2031**) within that window, then re-validate against a later LTSC release.

## Release notes

> New release notes must follow the [release-notes template](release-notes-template.md), which includes a mandatory **`API changes`** section.

- [v1.9.2](release_v1.9.2.md)
- [v1.9.1](release_v1.9.1.md)
- [v1.9.0](release_v1.9.0.md)
- [v1.8.3](release_v1.8.3.md)
- [v1.8.2](release_v1.8.2.md)
- [v1.8.1](release_v1.8.1.md)
- [v1.8.0](release_v1.8.0.md)
- [v1.7.2](release_v1.7.2.md)
- [v1.7.0](release_v1.7.0.md)
- [v1.6.7](release_v1.6.7.md)
- [v1.6.6](release_v1.6.6.md)
- [v1.2.10](release_v1.2.10.md)
- [v1.2.9](release_v1.2.9.md)
- [v1.1.4](release_v1.1.4.md)
- [v1.1.3p2](release_v1.1.3p2.md)
