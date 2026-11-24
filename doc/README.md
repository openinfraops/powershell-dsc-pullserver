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
- [Configuration reference](Configuration.md) — exhaustive per-key reference of every section (`database`, `dsc_pullserver`, `web_ui`, `saml`) with example files.
- [Database schema](DatabaseSchema.md) — supported databases, schema files, every table, relationships, schema evolution.
- [API v1 reference](APIv1.md) — the REST endpoints under `/api/v1/` (auth, users, agents, DSC settings, audit, modules, properties, configurations, SAML).
- [SAML integration](SAML.md) — SAML single-sign-on configuration.

## Protocol reference (spec)

- [MS-DSCPM] Desired State Configuration Pull Model Protocol: [`[MS-DSCPM].pdf`](%5BMS-DSCPM%5D.pdf) (reference specification, v20240423).

## Release notes

- [v1.7.2](release_v1.7.2.md)
- [v1.7.0](release_v1.7.0.md)
- [v1.6.7](release_v1.6.7.md)
- [v1.6.6](release_v1.6.6.md)
- [v1.2.10](release_v1.2.10.md)
- [v1.2.9](release_v1.2.9.md)
- [v1.1.4](release_v1.1.4.md)
- [v1.1.3p2](release_v1.1.3p2.md)
