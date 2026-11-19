# Release v1.2.10

Date: 2026-11-19

Release fixing the scheduled configuration repository sync being triggered on every scheduler tick even when nothing changed in the Git checkout.

## Changes

### Configuration repository sync

- Fixed `internal/service/configuration_repo.go` so the scheduled repository sync only recompiles and re-imports configurations when the Git checkout actually changed (fresh clone or fast-forward). Previously `syncGitRepositoryContext` returned only an error, so `SyncAndCompileConfigurationsForNodeContext` always ran the PowerShell compilation and `ImportCompiledConfigurationsAndAssign` re-imported every configuration on each tick. Because the generated MOF is not byte-stable across runs, this bumped configuration versions and reset agents to `pending_apply` on every sync even when no file was updated.
- `syncGitRepositoryContext` and `SyncGitRepository` now report whether the checkout changed (`bool`). `SyncAndCompileConfigurationsForNodeContext` skips compilation when the checkout is unchanged and no node-specific context is required, so configurations are only updated when the repository actually changed.
- Node registration still compiles unconditionally (a newly registered node must be compiled even if the repository is up to date).

Impacted files:

- internal/service/configuration_repo.go
- internal/service/configuration_repo_git_test.go

## Tests

- Added `TestSyncGitRepositoryReportsChange` verifying that a clone reports a change, an up-to-date sync reports no change, and a sync after a new remote commit reports a change.
