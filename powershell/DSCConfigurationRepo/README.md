# DSC Configuration Repository

This directory is a starter repository layout. In production, put these files in a dedicated Git repository and point the pull server configuration at its clone URL and branch.

## Layout

- Le compilateur détecte aussi le layout historique `configs/definitions` + `configs/configurations`. Il charge alors les PSD1 sous `definitions/os`, `environnement`, `hostgroup`, `application` et `node`, puis fusionne dans cet ordre : OS, environnement, hostgroups, application, composant, nœud. Les entrées ayant la même clé `function::resource` et le même `Name` sont remplacées par la couche suivante.
- `configurations/*.psd1` declares a layer for a configuration family: `ConfigurationName`, `Priority`, optional `Match` tag selectors, module, optional `RequiredModules`, function name, and parameter data.
- `modules/*.psm1` contains the DSC `Configuration` functions.
- `scripts/Compile-Configurations.ps1` selects matching layers, sorts by ascending `Priority` then `Name`, merges parameter data recursively, and writes one canonical MOF per configuration family.

Layers for a family must use the same PowerShell module and `Configuration` function. Later layers override scalar values and entries with the same `Name` in arrays. Resource data is grouped under keys formatted as `function::DscResource`, for example `default::WindowsFeature`; the function prefix dispatches to a PowerShell function in the module, and the resource suffix describes the DSC resource type that function declares. Use the `Match` keys `OS`, `Environnement`, `Hostgroup`, `Application`, `Composant`, and `NodeName` to align selectors with node tags. An omitted or empty `Match` applies as a base layer. Assign the `ConfigurationName` to agents.

## Requirements

Run the pull server on Windows with PowerShell 7 (`pwsh`) installed. Git transport and checkout are implemented in Go with `go-git`; a separate `git.exe` installation is not required. The service account needs network access to the repository and write access to the configured checkout directory. DSC resources referenced by a configuration must be available to that same PowerShell installation. Declare them in each manifest's `RequiredModules`; the compiler imports those modules before importing the PSM1. The example uses `RequiredModules = @('cChoco')`, which must be installed on the server before compiling its Sources and Packages groups.

The compiler script is executable code from the configured repository. Only use a trusted repository and protect its write permissions. Public HTTPS and local repositories need no extra credentials. For private repositories, `go-git` needs an explicitly configured authentication method; it does not automatically use Git Credential Manager or the Windows SSH agent. Do not embed credentials in the repository.

## Scheduler administration

Configure the repository URL, branch, checkout path, compiler script, PowerShell executable, synchronization interval, and optional compile-on-registration behavior from the admin Scheduler page. These values are persisted in the `configuration_repo_settings` database table. Relative checkout paths resolve from the server executable directory. The sync task runs asynchronously at startup and then at the configured interval. Administrators can also run it from the Scheduler page or `POST /api/v1/scheduler/configuration-repo/run`.

When compile-on-registration is enabled, a new agent compiles its assigned configuration names using a temporary JSON context containing the node name, assigned configuration names, and server-side tags. The compiler selects matching layers and overrides the merged `NodeName` parameter with the registering node's name. If no configuration is assigned, the node name is used as the configuration name. Existing agents that re-register do not trigger this behavior.

For the historical layout, the node tags must include `OS`, `Environnement`, `Application`, `Composant`, and `Hostgroup` (one or more values); aliases such as `OperatingSystem`, `Environment`, and `Component` are accepted. Hostgroups may also be supplied as a comma- or semicolon-separated value. This console stores generic tags and does not infer these values from the legacy infrastructure database, so populate the tags before compiling. The scheduler refresh compiles one node-specific MOF for each registered node and associates the MOF name with its agent; DSC checks the generated MOF checksum on its next poll.
