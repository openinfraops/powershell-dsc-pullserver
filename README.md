# go-dsc-pull

A secure, versioned DSC Pull Server with a modern web interface, written in Go.

## Purpose

Centralized management of DSC configurations, modules, users, and reports, with advanced access controls and a user-friendly web UI. It implements the [MS-DSCPM] pull-server protocol for Windows DSC nodes (LCM) and exposes a REST API plus a PowerShell module for remote management.

## Documentation

Detailed documentation is organized into two main sections:

- [WEB UI Interface](doc/webui/README.md)
- [DSC Pull Server](doc/dscpull/README.md)

## Quick Navigation

- [Installation](doc/installation.md)
- [Configuration reference](doc/Configuration.md) (exhaustive, all sections)
- [Database schema](doc/DatabaseSchema.md)
- [DSC Pull Server configuration guide](doc/dscpull/configuration.md) (focused, mutual-TLS)
- [Configuration repository (Git) & Scheduler](doc/dscpull/configuration-repo.md)
- [Node Registration](doc/dscpull/registration.md)
- [Security](doc/dscpull/security.md)
- [API v1 Reference](doc/APIv1.md)
- [SAML Integration](doc/SAML.md)
- [Web Authentication](doc/webui/authentication.md)
- [Resource Modules](doc/webui/modules.md)
- [MOF Configurations](doc/webui/configurations.md)
- [Roles & Access](doc/webui/permissions.md)
- [Storage Modes](doc/webui/storage.md)

## Getting started

The server ships as a single self-contained **Go** executable — no Go runtime is needed on the target host. Grab the latest pre-built binary from the [GitHub Releases page](https://github.com/openinfraops/powershell-dsc-pullserver/releases) (the release zip keeps the required `web/`, `templates/` and `db/` directories).

To install or update on Windows, use the provided scripts (run as Administrator):

```powershell
# install from the extracted release folder; -CreateService registers the Windows service
./scripts/install-windows.ps1 -SourcePath <extracted-release-folder> -CreateService

# update an existing installation
./scripts/update-windows.ps1 -SourcePath <new-extracted-release-folder>
```

Then:

- Edit `config.json` (database, SAML, web UI, DSC settings) — use [`config_example.json`](config_example.json) as a starting point.
- Bootstrap the initial admin account: `DSC-Admin-Console.exe init-bootstrap-admin`.
- See the full [installation guide](doc/installation.md) and [configuration reference](doc/Configuration.md).

To build the binary yourself, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Contributing

Contributions to the code or documentation are welcome. Open an issue for bug reports and ideas; to build, test and open a pull request, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT License
