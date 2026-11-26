# Integrations

This page documents the third-party integrations that consume the DSC Pull Server REST API, the compatibility contract between this server and each integration, and the permissions an API token must carry for each one.

Integrations rely on the **API stability** of this server: a change that breaks an integration's requests is a breaking change and must be treated with care (see [API versioning](#api-versioning--breaking-changes)).

---

## Terraform / OpenTofu provider

[Terraform / OpenTofu Provider: `dscpull`](https://github.com/openinfraops/terraform-provider-dscpull) provisions DSC Pull Server nodes declaratively. It manages a single resource, `dscpull_node`, with the full Create / Read / Update / Delete / Import lifecycle, and it authenticates with an API token (`Authorization: Token <api_token>`).

- Provider source: `openinfraops/dscpull`
- Resource: `dscpull_node`
- Authenticated via an [API token](APIv1.md#usage-of-jwt-and-api-tokens) (`Authorization: Token <token>`), not a JWT.

### Endpoints the provider uses

The provider's resource lifecycle maps to the following [`/api/v1`](APIv1.md) endpoints:

| Lifecycle | Endpoint | Auth |
| --- | --- | --- |
| Create | `POST /api/v1/agents/preenroll` | any authenticated user |
| Create / Update | `POST /api/v1/agents/{id}/configs` | any authenticated user |
| Read | `GET /api/v1/agents` , `GET /api/v1/agents/{id}` | any authenticated user |
| Read (detail) | `GET /api/v1/agents/{id}/configs` , `GET /api/v1/agents/{id}/tags` | any authenticated user |
| Update (tags) | `PUT /api/v1/agents/{id}/tags` , `DELETE /api/v1/agents/{id}/tags` | **admin only** |
| Delete | `DELETE /api/v1/agents/{id}` | **admin only** |

> [Roles & Access](webui/permissions.md) defines two roles: `admin` and `user`. The write operations on tags and the node deletion are restricted to `admin`.

### Minimal role for the Terraform API token

Because tag synchronization and node deletion are **admin-only**, the API token used by Terraform must belong to a user with the **`admin`** role to drive the full resource lifecycle. A plain `user`-role token can pre-enroll and assign configs, but it **cannot** set tags or delete a node, so Terraform would fail with `401 Unauthorized`/`403 Forbidden` on those operations.

> **Recommendation.** Create a dedicated, non-interactive admin account for Terraform and use only its API token in the provider configuration (`base_url`/`token`). Do not reuse a personal admin's token, and rotate it via the [user tokens endpoints](APIv1.md#users) (`POST /api/v1/users/{id}/tokens`, `POST /api/v1/users/{id}/tokens/{tokenid}/revoke`). Because the server exposes only the `admin` and `user` roles, `admin` is both the **minimum** role that drives the full node lifecycle and the only option — there is no finer-grained read/write role for these specific operations.

---

## API compatibility matrix

The API is versioned by its URL prefix (`/api/v1/`) — see [API versioning](#api-versioning--breaking-changes). The matrix below maps the referenced server version to the provider version that is known to work against it.

### Server → provider compatibility

| DSC Pull Server | Provider version | API prefix | Compatibility |
| --- | --- | --- | --- |
| **v1.7.x** (current) | **v0.1.0** (planned, not yet released) | `/api/v1` | Compatible with a caveat (see below) |

> **Provider status.** The Terraform provider (`openinfraops/terraform-provider-dscpull`) is still in development: it currently has **no tagged release**. The mapping above references its intended `v0.1.0`. Once the provider is released, this table is the place to pin the tested server ↔ provider pair, and to record any API change that required a coordinated provider update.

### `internal_dsc_id` contract (caveat)

The provider is built around an **immutable node identity**, `internal_dsc_id`, as the Terraform resource ID. `agent_id` is treated as a runtime-only identifier that may change (for example `TEMP-*` → GUID after registration).

- The provider reads `internal_dsc_id` from the agent payloads (`GET /api/v1/agents`, `GET /api/v1/agents/{id}`).
- **Current server (v1.7.x) does not emit `internal_dsc_id`** in those payloads. Until it does, the provider **falls back to `agent_id`** and emits an explicit warning about this contractual debt.
- The required server-side change is **additive and non-breaking**: add `internal_dsc_id` to the agent JSON in `GET /api/v1/agents`, `GET /api/v1/agents/{id}`, and ideally `POST /api/v1/agents/preenroll`. See the provider's README for the expected payload shape.

Adding this field restores the stable-identity guarantee without breaking any existing client.

---

## API versioning & breaking changes

- The REST API is versioned by URL prefix: `/api/v1/`. The **major version** (`1`) is a stability contract.
- **Additive** changes (new endpoints, new optional JSON fields, new query parameters) are allowed within a major version and do not require a provider change.
- **Breaking** changes (renaming/removing a field or endpoint, changing a field type or semantics, tightening auth on an endpoint) **must not** be introduced silently within `/api/v1/`.

When a breaking change is unavoidable, it must:

1. Be recorded in the release note's **`API changes`** section (see the [release-note template](release-notes-template.md)).
2. Bump the API path to a new major version (for example `/api/v2/`) or ship in lockstep with a coordinated provider release.
3. Be communicated to integration maintainers (the [Terraform provider](https://github.com/openinfraops/terraform-provider-dscpull)) before the release.

---

## Zabbix template

A Zabbix template for monitoring the pull server (via the unauthenticated [`/health`](dscpull/configuration.md#monitoring-endpoints) and gated `/metrics` endpoints) is planned. It will be linked here when published.

---

## See also

- [API v1 Endpoints Reference](APIv1.md) — the full list of REST endpoints.
- [Roles & Access](webui/permissions.md) — the `admin` / `user` roles and what each can do.
- [DSC Pull Server configuration](dscpull/configuration.md) — monitoring endpoints, base URL, and server configuration.
