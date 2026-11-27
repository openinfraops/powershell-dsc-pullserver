# Permissions (admin/users)

This page details the access control and user roles in the web interface.

## Roles
- Admin: Full access to all features
- User: Limited access (view, apply configurations)

## Access Control
- Admin-only actions are protected
- Role mapping via SAML/JWT

### Admin-only operations (enforced server-side)
- **Tag modification** — `PUT`/`DELETE /api/v1/agents/{id}/tags` and `PUT /api/v1/agents/bulk/tags` are **admin only** in the API (a `user`-role account gets `403`).
- **Pre-enrollment** — `POST /api/v1/agents/preenroll` is **admin only** (it can supply tags).
- **Node registration** — a node **cannot** set tags at registration (`PUT`/`POST /PSDSCPullServer.svc/{node}` never parses tag fields). Tags come only from admins, pre-enrollment, and token/policy rules.

## Security
- All actions are logged
- Permissions are enforced at API and UI levels.