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
- [Configuration reference](doc/Configuration.md)
- [Database schema](doc/DatabaseSchema.md)
- [DSC Pull Server Configuration](doc/dscpull/configuration.md)
- [Configuration repository (Git) & Scheduler](doc/dscpull/configuration-repo.md)
- [Node Registration](doc/dscpull/enregistrement.md)
- [Security](doc/dscpull/securite.md)
- [API v1 Reference](doc/APIv1.md)
- [SAML Integration](doc/SAML.md)
- [Web Authentication](doc/webui/authentification.md)
- [Resource Modules](doc/webui/modules.md)
- [MOF Configurations](doc/webui/configurations.md)
- [Roles & Access](doc/webui/droits.md)
- [Storage Modes](doc/webui/stockage.md)

## Build (Windows)

To generate a Windows executable with:

- metadata visible in the file properties (Product Version, File Version, etc.)
- build metadata injected into the logs (version, commit, date)

use:

```powershell
./scripts/build-windows.ps1
```

The `build/version.json` file is read automatically by default and serves as the local source of versioning and product metadata.

From a non-Windows environment you can also target Windows by setting `GOOS=windows` and `GOARCH` before running the script (PowerShell 7 required).

## Contribution

To contribute, open an issue or submit a pull request. Any help to improve the documentation or code is welcome.

## License

MIT License
