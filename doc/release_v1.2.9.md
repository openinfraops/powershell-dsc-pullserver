# Release v1.2.9

Date: 2026-11-19

Release fixing API token authentication for admin-only endpoints and updating dependencies.

## Changes

### Authentication

- Fixed `internal/auth/isadmin.go` so `IsAdmin` recognizes requests authenticated with an API token (`Authorization: Token <token>`). Previously only JWT (Bearer header or cookie) was recognized, so API-token-authenticated calls to admin-only endpoints (e.g. agent tags) always returned `403 Forbidden: admin only`, even for admin accounts. The user is now resolved via `db.CheckAPIToken` and the `role` is checked by user id; the JWT/cookie path is unchanged.

Impacted files:

- internal/auth/isadmin.go

### Dependencies

- Bumped `actions/checkout` from 6 to 7.
- Bumped `actions/setup-go` from 6 to 7.
- Bumped `golang.org/x/crypto` from 0.47.0 to 0.52.0.

Impacted files:

- .github/workflows/release.yml
- go.mod
- go.sum
