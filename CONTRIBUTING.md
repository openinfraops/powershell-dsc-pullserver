# Contributing

Thanks for your interest in **go-dsc-pull**. Feedback, bug reports and contributions to the code or documentation are welcome.

## Reporting issues

- Open an issue describing the problem: steps to reproduce, expected vs actual behaviour.
- For **security** issues, prefer a private report rather than a public issue.

## Submitting changes

- Open a pull request from a feature branch off `main`.
- Keep changes focused and consistent with the surrounding code style and naming.
- Update the relevant documentation (`doc/`) when behaviour or configuration changes; if you add a new page, reference it from the [documentation index](doc/README.md).
- When the change is user-facing, update the **latest** [release note](doc/README.md#release-notes) (the newest `doc/release_v*.md`), not a historical one. Follow the [release-notes template](doc/release-notes-template.md), and always include the mandatory **`API changes`** section (write "No API changes" if the change does not touch the REST API).

## Development environment

Requires [Go](https://go.dev/dl/) (the version used by CI is pinned in `.github/workflows/release.yml`).

### Build (Windows)

To generate a Windows executable with:

- metadata visible in the file properties (Product Version, File Version, etc.)
- build metadata injected into the logs (version, commit, date)

run from the repository root:

```powershell
./scripts/build-windows.ps1
```

The `build/version.json` file is read automatically and serves as the local source of versioning and product metadata.

Useful parameters:

| Parameter | Description |
|-----------|-------------|
| `-Output` | Output file name (default `DSC-Admin-Console.exe`). |
| `-VersionFile` | Path to the version/metadata JSON file (default `build/version.json`). |
| `-CommitOverride` | Commit SHA injected into the build info (defaults to `HEAD`). |

From a non-Windows environment you can target Windows by setting `GOOS=windows` and `GOARCH` before running the script (PowerShell 7 required).

### Check & test

Before opening a pull request, run:

```powershell
go build ./...
go vet ./...
go test ./...
```

## License

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE).
