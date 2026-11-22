# Configuration Repository (Git) & Scheduler

This page describes how the DSC Pull Server syncs a **Git-backed configuration repository**, compiles it into MOF files, and imports them into the configuration model store. These settings are **stored in the database** (`configuration_repo_settings`) and managed from the **Scheduler administration page** (`/web/admin/scheduler`) or the admin API — they are **not** edited in `config.json`.

> The `configuration_repo_*` keys that exist in `config.json` are a **legacy migration source only**. On first startup they are read once into the `configuration_repo_settings` table (`legacy_imported`), after which the Scheduler page is the source of truth.

---

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/scheduler/config` | Read the scheduler + configuration-repo settings (admin) |
| `PUT` | `/api/v1/scheduler/config` | Save the scheduler + configuration-repo settings (admin) |
| `POST` | `/api/v1/scheduler/configuration-repo/run` | Trigger a manual sync + compile (admin) |
| `GET` | `/api/v1/scheduler/tasks` | List scheduled background tasks (admin) |
| `GET` | `/api/v1/scheduler/tasks/{task}/history` | Task execution history (admin) |

Web UI: the settings live on **`/web/admin/scheduler`** (admin only).

---

## Settings

| Key | Type | Default | Description |
|---|---|---|---|
| `enable_configuration_repo_sync` | bool | `false` | Enable the scheduled repository sync. |
| `configuration_repo_url` | string | `""` | Git repository URL (HTTPS). Used with optional credentials below. |
| `configuration_repo_branch` | string | `"main"` | Branch to sync. |
| `configuration_repo_directory` | string | `"data/configuration-repository"` | Local directory where the repository is checked out. |
| `configuration_compiler_script` | string | `"scripts/Compile-Configurations.ps1"` | Compiler script (relative to the checkout root) that produces `.mof` files. |
| `configuration_powershell_executable` | string | `"pwsh"` | PowerShell executable used to run the compiler script (PowerShell 7). |
| `configuration_repo_username` | string | `""` | Git username (optional). |
| `configuration_repo_password` | string | `""` | Git password / personal access token (optional). |
| `configuration_repo_sync_interval_mins` | int | `60` | Scheduled sync interval, in minutes. |
| `compile_configuration_on_node_registration` | bool | `false` | Also compile and import when a **new node registers** (in addition to the scheduled sync). |

---

## Compiler contract

The sync flow:

1. Clones/fetches the configured `configuration_repo_branch` into `configuration_repo_directory`.
2. Runs the compiler script with the configured `configuration_powershell_executable`:

   ```powershell
   pwsh -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass \
     -File <compiler_script> -SourcePath <checkout_dir> -OutputPath <temp_output>
   ```

3. The compiler must write **one `.mof` file per configuration** into `<temp_output>`. The file name maps to the configuration name; **names must be unique** (duplicate names are rejected).
4. The generated MOFs are imported into the configuration model store, **versioned**, and agents assigned to a changed configuration are marked `pending_apply` so they receive the update.

> **Compilation only runs when the Git checkout actually changed** (fresh clone or fast-forward). Because the generated MOF is not byte-stable across runs, recompiling an unchanged repository would otherwise bump every configuration version and reset agents to `pending_apply` on every tick. A newly registered node always compiles, even when the repository is current.

### Requirements

- **Windows** and **PowerShell 7 (`pwsh`)** on the server.
- The DSC resource modules referenced by the configurations must be installed for that PowerShell environment.
- The service account needs **network access** to the Git repository and **write access** to `configuration_repo_directory` and the temporary output directory.
- **Use only a trusted repository** — the configured compiler script is **executable code** run server-side.

See [`powershell/DSCConfigurationRepo/README.md`](../../powershell/DSCConfigurationRepo/README.md) for a starter repository layout (manifest, modules, compiler script) and its priority rule.
