# Release vX.Y.Z

Date: YYYY-MM-DD

One-to-two sentence summary of the release. Replace `vX.Y.Z` and the date with the actual version and release date. When a fix supersedes an unpublished intermediate version (for example `v1.7.1`), say so here.

## API changes

**Copy and adapt this section — it is mandatory.** Record every change that affects the REST API under `/api/v1/`, because external integrations (the [Terraform provider](Integrations.md)) depend on it. State whether each change is **additive** or **breaking**.

- *Additive:* `GET /api/v1/...` — what it does.
- *Breaking:* `PUT /api/v1/...` — field renamed `x` → `y`. Coordinate with the provider.

If there are no API changes, write: **No API changes.**

## Changes

### Feature / fix heading

Describe the change in plain language. Include the motivation, the user-visible effect, and any operational note. Use one `###`-heading per meaningful change.

## Impacted files

- `path/to/file.go`
- `path/to/file_test.go`
- `doc/...md`

> **When writing a release note**
> - Keep the same structure as the previous release notes in this folder.
> - Update the **latest** `doc/release_v*.md` (the newest) for in-progress work — never a historical one.
> - Always include the `API changes` section (write "No API changes" if none).
> - List every impacted file in `## Impacted files`.
> - Reference this file from the [documentation index](README.md#release-notes).
