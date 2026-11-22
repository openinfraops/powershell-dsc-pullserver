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
	- `tag=key:value`: filter by tag.
	- Repeat `tag` multiple times to require all tags with `AND` semantics, for example: `/api/v1/agents?tag=env:prod&tag=role:web`.
- Response objects may include `tags`, returned as a map of tag keys to arrays of values.

### POST `/api/v1/agents/preenroll`
- Pre-enroll an agent.

### GET `/api/v1/agents/{id}`
- Get agent details.
- Response may include `tags`, returned as a map of tag keys to arrays of values.

### GET `/api/v1/agents/{id}/configs`
- List configs assigned to the agent.

### POST `/api/v1/agents/{id}/configs`
- Add a config to the agent.

### DELETE `/api/v1/agents/{id}/configs`
- Remove a config from the agent.

### GET `/api/v1/agents/{id}/reports`
- List agent's reports.

### GET `/api/v1/agents/{id}/reports/latest`
- Get latest report for the agent.

### GET `/api/v1/agents/{id}/reports/{jobid}`
- Get report for a specific job.

### GET `/api/v1/agents/{id}/tags`
- List agent's tags.

### PUT `/api/v1/agents/{id}/tags`
- Set agent's tags.

### DELETE `/api/v1/agents/{id}/tags`
- Remove all agent's tags.

---

## Audit

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

## Notes
- All endpoints (except login) require a valid JWT in `Authorization: Bearer <token>`.
- Admin endpoints require admin role.
- For more details, see the code in `internal/routes/web_routes.go` and related handlers.
