# API v1 Endpoints Reference

This document lists the REST endpoints exposed under `/api/v1/` by the DSC Pull Server. It covers user, agent, configuration, module, property, SAML, and authentication management.

---

## Authentication

### POST `/api/v1/login`
- Authenticates a user, returns a JWT.
- Body: `{ "username": "...", "password": "..." }`
- Response: `{ "token": "..." }`

> **Brute-force protection:** this endpoint is rate-limited per client IP. After **5 failed attempts** within a **15-minute window**, further attempts from that IP return **HTTP 429** with `Retry-After: 900`. A successful login resets the counter.

#### Usage of JWT and API Tokens

After successful login, include the JWT in the `Authorization` header for all protected API requests:

```
Authorization: Bearer <your-jwt-token>
```

Alternatively, you can use an API token (created via the user tokens endpoints) in the same header:

```
Authorization: Token <your-api-token>
```

Both JWT and API tokens grant access to the API according to the user's permissions. Never share your tokens. Tokens should be sent with every request to endpoints that require authentication.

---

## Users

### GET `/api/v1/users`
- List all users (admin).

### GET `/api/v1/users/{id}`
- Get user details.

### POST `/api/v1/users`
- Create a user.

### PUT `/api/v1/users/{id}`
- Update a user.

### DELETE `/api/v1/users/{id}`
- Delete a user.

### POST `/api/v1/users/{id}/active`
- Activate/deactivate a user.

### POST `/api/v1/users/{id}/password`
- Change user password.

### GET `/api/v1/users/{id}/tokens`
- List user's API tokens.

### POST `/api/v1/users/{id}/tokens`
- Create an API token.

### POST `/api/v1/users/{id}/tokens/{tokenid}/revoke`
- Revoke an API token.

### DELETE `/api/v1/users/{id}/tokens/{tokenid}`
- Delete an API token.

---

## Roles & Profile

### GET `/api/v1/user_roles`
- List available user roles.

### GET `/api/v1/my`
- Get current account info.

---

## Agents

### GET `/api/v1/agents`
- List all agents.
- Query params supported:
	- `count=1`: return only the total count.
	- `stats` (ex: `/api/v1/agents?stats`): return agent counts by status buckets:
		- `compliant` (`state` in `success|ok`)
		- `failed` (`state=failure`)
		- `pending_enroll` (`state=waiting_for_registration`)
		- `pending_apply` (`state=pending_apply`)
	- `node_name=<name>`: filter by exact node name.
	- `has_error_last_report=true|false`: filter by last report error status.
	- `reboot_pending=true|false`: filter by whether the node currently has a reboot pending.
	- `reboot_loop=true`: filter by whether the node is flagged in a reboot loop.
	- `tag=key:value`: filter by tag.
	- Repeat `tag` multiple times to require all tags with `AND` semantics, for example: `/api/v1/agents?tag=env:prod&tag=role:web`.
- Response objects may include `tags`, returned as a map of tag keys to arrays of values.
- Response objects include the derived reboot state of the node:
	- `reboot_pending` (boolean): whether the node has requested a reboot in its last report.
	- `reboot_pending_since` (string, optional): timestamp of the first report of the current pending streak.
	- `reboot_requested_by` (string, optional): identifier of the resource that requested the reboot, in the form `<ModuleName|SourceInfo>:<ResourceId>`.
	- `reboot_loop` (boolean): whether the node has requested a reboot on N consecutive reports (default 3) over at least M minutes (default 60), i.e. it is stuck in a reboot loop.
	- `reboot_request_count` (integer): number of consecutive reports with the reboot flag set.
- The `stats` response additionally includes `reboot_pending` (nodes currently pending a reboot) and `reboot_loop` (nodes flagged in a reboot loop) counters.

### POST `/api/v1/agents/preenroll`
- **Admin only.** Pre-enroll an agent before its first registration, giving it a temporary `TEMP-*` id and optionally applying initial tags (e.g. `environment`) that the first configuration compile needs.
- Request body: `{ "node_name": "...", "tags": [ { "key": "...", "value": "..." } ] }`.
- The tags supplied here are audited (actor, source IP, applied tags) with the `preenroll` action.
- **Nodes cannot self-tag:** the DSC registration endpoint (`RegisterHandler`, `PUT`/`POST /PSDSCPullServer.svc/{node}`) never parses or stores tags. Only admins, pre-enrollment, and token/policy rules can set tags.

### GET `/api/v1/agents/{id}`
- Get agent details.
- Response may include `tags`, returned as a map of tag keys to arrays of values.
- Response also includes `force_cert_rotation` (boolean) — set when an admin has requested an ASAP certificate renewal.
- Response includes the derived reboot state of the node (`reboot_pending`, `reboot_pending_since`, `reboot_requested_by`, `reboot_loop`, `reboot_request_count`) — see the same fields under `GET /api/v1/agents`.

### POST `/api/v1/agents/{id}/rotate-certificate`
- **Admin only.** Ask the node to rotate its client certificate on its **next** cycle (sets `agents.force_cert_rotation` to `1`). The server then emits `DSC-certificateRotation: True` on the node's next pull, even outside the renewal window and even if the global certificate-rotation setting is off.

### DELETE `/api/v1/agents/{id}/rotate-certificate`
- **Admin only.** Cancel a pending ASAP certificate-renewal request (clears `agents.force_cert_rotation`).

### GET `/api/v1/agents/{id}/configs`
- List configs assigned to the agent.

### POST `/api/v1/agents/{id}/configs`
- Add a config to the agent.

### DELETE `/api/v1/agents/{id}/configs`
- Remove a config from the agent.

### GET `/api/v1/agents/{id}/reports`
- List agent's reports.
- Each report summary includes `reboot_requested` (boolean): whether the corresponding run requested a reboot (accepts the DSC protocol's `"True"`/`"False"` and `"1"`/`"0"` values).
- Query params supported:
	- `operationtype=<type>`: filter by operation type.
	- `mofapplied=<0|1>`: filter by whether a MOF was applied.

### GET `/api/v1/agents/{id}/reports/latest`
- Get latest report for the agent.

### GET `/api/v1/agents/{id}/reports/{jobid}`
- Get report for a specific job.

### GET `/api/v1/agents/{id}/tags`
- List agent's tags.

### PUT `/api/v1/agents/{id}/tags`
- **Admin only.** Add a value to a tag key for an agent (multiple values are allowed for the same key).
- Request body: `{ "key": "...", "value": "..." }`.
- Audited (actor, source IP, previous vs. new value) with the `tag.set` action.

### DELETE `/api/v1/agents/{id}/tags`
- **Admin only.** Remove a specific tag value (via `?key=&value=` or a JSON body `{ "key": ..., "value": ... }`), or remove all tags when neither is provided.
- Audited (actor, source IP, removed/previous value) with the `tag.delete` action.

### PUT `/api/v1/agents/bulk/tags`
- **Admin only.** Apply a tag key/value to a list of agents (`{ "agent_ids": [...], "key": "...", "value": "..." }`).
- Audited (actor, source IP, before/after per agent) with the `tag.set` action.

---

## DSC Settings

### GET `/api/v1/dsc/settings`
- **Admin only.** Read the current DSC settings stored in the `dsc_settings` table (single row `id=1`).
- Response: `{ "certificate_rotation_enabled": boolean, "certificate_renewal_days": int }`.

### PUT `/api/v1/dsc/settings`
- **Admin only.** Update the DSC settings.
- Body: `{ "certificate_rotation_enabled": boolean, "certificate_renewal_days": int }`.
- `certificate_renewal_days` must be `> 0` (otherwise `400`).
- Stored in the database — **no service restart required**.

> **Scope:** these two settings control server-initiated certificate rotation. `certificate_rotation_enabled` (default `false`) turns on the `DSC-certificateRotation: True` response header for nodes whose certificate is inside the renewal window; `certificate_renewal_days` (default `30`) is that window in days before `certificate_notafter`. See [DSC Security](dscpull/security.md) and [Configuration](dscpull/configuration.md).

---

## Audit

> For the full description of the audit feature — audited events, stored fields,
> retention, access control and the review of gaps — see the
> [Audit trail](Audit.md) page.

### GET `/api/v1/audit`
- List audit rows (paginated, DataTables compatible).
- Query params supported (combinable):
	- `users=user1,user2` (emails)
	- `actions=login,delete`
	- `date_from=YYYY-MM-DDTHH:mm` (or `YYYY-MM-DD HH:mm:ss`)
	- `date_to=YYYY-MM-DDTHH:mm` (or `YYYY-MM-DD HH:mm:ss`)
	- DataTables params: `draw`, `start`, `length`, `search[value]`, `order[0][column]`, `order[0][dir]`

### GET `/api/v1/audit/filters`
- Returns available values for audit filter dropdowns.
- Response shape:
	- `users`: string[]
	- `actions`: string[]

### GET `/api/v1/audit/export`
- Export audit rows as CSV.

---

## Modules

### POST `/api/v1/modules/upload`
- Upload a PowerShell module.

### GET `/api/v1/modules`
- List available modules.

### DELETE `/api/v1/modules/delete`
- Delete a module.

---

## Properties

### GET `/api/v1/properties`
- List all properties.

### POST `/api/v1/properties`
- Create a property.

### GET `/api/v1/properties/{id}`
- Get property details.

### PUT `/api/v1/properties/{id}`
- Update a property.

### DELETE `/api/v1/properties/{id}`
- Delete a property.

---

## Configuration Models

### POST `/api/v1/configuration_models`
- Create a configuration model (admin only).

### GET `/api/v1/configuration_models`
- List all models.

### GET `/api/v1/configuration_models/{id}`
- Get model details.

### PUT `/api/v1/configuration_models/{id}`
- Update a model (admin only).

### DELETE `/api/v1/configuration_models`
- Delete one or more models (admin only).

> **Creating or deleting configuration models is restricted to administrators** (the `admin` role). This prevents low-privileged users from poisoning or removing configuration MOFs that are served to DSC nodes. `PUT` (update) and the model **upload** path are also admin-only.

---

## SAML

### GET `/api/v1/saml/userinfo`
- Get SAML info for the current user.

### GET `/api/v1/saml/enabled`
- Get SAML enabled status.

### GET `/api/v1/saml/config`
- Get SAML config (admin).

### PUT `/api/v1/saml/config`
- Update SAML config (admin).

### POST `/api/v1/saml/upload_sp_keycert`
- Upload SAML SP key/cert (admin).

---

## Error Groups

Groups DSC report errors by a normalized fingerprint so repeated occurrences of the same problem collapse into a single tracked issue. Errors are extracted from the `status_data` JSON of each report and classified into categories. Each group carries detection flags (spike / new / after-deployment) and a trend.

### GET `/api/v1/error_groups`
- List error groups, ordered by node count then most recently seen.
- Query params supported:
	- `status=<open|known|resolved>`: filter by status.
	- `category=<name>`: filter by category.
	- `since=<YYYY-MM-DD|ISO datetime>`: only groups first seen at or after this time.
	- `min_nodes=<n>`: only groups affecting at least `n` distinct nodes.
	- `tag=<key:value>`: only groups affecting a node carrying that tag. Repeatable.
	- `limit=<n>` (default `20`, max `200`) and `offset=<n>`: pagination.
- Response: `{ "groups": [ ... ], "total": <n>, "offset": <n>, "limit": <n> }`.
- Each item has `fingerprint`, `category`, `resource_type`, `sample_message` (HTML-escaped), `first_seen`, `last_seen`, `first_config_version`, `status` (`open`/`known`/`resolved`), `status_changed_by`, `status_changed_at`, `note`, `node_count`, and the detection fields: `spike`, `new`, `after_deployment`, `trend` (`rising`/`stable`/`falling`), `node_count_last_hour`, `node_count_prev_hour`, `detection_alerted`, `detection_suppressed`.

### GET `/api/v1/error_groups/{fingerprint}`
- Get a single error group (same shape as a list item).
- `404` when the fingerprint is unknown.

### GET `/api/v1/error_groups/{fingerprint}/nodes`
- List the affected nodes and their occurrence counters for a group, most recently seen first.
- Query params: `limit` (default `100`), `offset`.
- Response: `{ "nodes": [ { "node_id": "...", "first_seen": "...", "last_seen": "...", "count": <n>, "last_report_id": <id> } ], "offset": <n>, "limit": <n>, "count": <n> }`.

### PUT `/api/v1/error_groups/{fingerprint}`
- **Admin only.** Update a group's triage state.
- Body: `{ "status": "known|resolved|open", "note": "..." }`.
- `status` is required; `note` is optional.
- Returns `204 No Content`. The change is audited (action `error_group.<status>`).
- `404` when the fingerprint is unknown.

---

## Notes
- All endpoints (except login) require a valid JWT in `Authorization: Bearer <token>`.
- Admin endpoints require admin role.
- See [Integrations](Integrations.md) for the API compatibility matrix, the API versioning & breaking-changes policy, and the **minimal role an API token must hold** for the Terraform provider.
- For more details, see the code in `internal/routes/web_routes.go` and related handlers.
