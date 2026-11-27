# Installation Guide

This page explains how to install and deploy the Go DSC Pull Server.

## Prerequisites

The server is a single self-contained **Go** executable (no Go runtime required on the target host). Before installing, ensure:

- **Operating system:** Windows is the primary target. The binary itself is cross-platform (the `init-bootstrap-admin` CLI and the web UI run anywhere), but the DSC config-repository **compilation** feature requires Windows + PowerShell (7 recommended, Windows PowerShell 5.1 works too).
- **Database:** either **Microsoft SQL Server** (MSSQL) or **SQLite** (embedded). Choose the driver in `config.json` → `database.driver`. An existing MSSQL database may require a migration script when upgrading (see `db/migration_*.sql`).
- **Windows PowerShell** — **only required** if you use the Git-backed **configuration repository** (compilation). The compiler script (`Compile-Configurations.ps1`) imports the `PSDesiredStateConfiguration` module, which ships with **Windows PowerShell 5.1** and is also available in **PowerShell 7 (`pwsh`)** — **either works**. `pwsh` is the default (`configuration_powershell_executable`), but you can point it at `powershell.exe` if only Windows PowerShell 5.1 is installed. Must be installed on the server and reachable by the service account. The DSC resource modules referenced by your configurations must be installed for that PowerShell environment.
- **Certificates:**
  - A **server certificate + private key** (PEM) for HTTPS, configured via `cert_file` / `key_file` (DSC and/or web UI). Use a certificate trusted by the clients.
  - For **client-certificate validation** (`enable_client_cert_validation: true`), the DSC endpoint operates in **mutual TLS (mTLS)**: each DSC node presents its **own client certificate** on every pull. See [DSC security](dscpull/security.md) for the thumbprint-binding model. mTLS requires HTTPS.
- **Registration key** (`dsc_pullserver.registrationKey`) — shared secret used by DSC nodes to register. Generate a strong value with [scripts/Generate-SecureKeys.ps1](../scripts/Generate-SecureKeys.ps1) (the same script generates the JWT signing secret for `web_ui.shared_secret`).
- **JWT signing secret** (`web_ui.shared_secret`, or `dsc_pullserver.shared_secret` for backward compatibility).
- **Network / ports:**
  - DSC pull server port (`8484` in the example configs).
  - Web UI / API port (`443` in the example configs).
  - Open these in the firewall. In HTTPS mode bind the ports to a trusted certificate.
- **Service account** (when installing as a Windows service): a least-privilege account is recommended (see [Best practices](#windows-deployment-best-practices)). It needs **read** access to the executable, `config.json`, the certificates, and the `web/`, `templates/`, and `db/` (schema) directories; and **write** access to the **installation directory** (where the SQLite `dsc.db` file and the `dsc-pull-server.log` file are created) and the configuration-repository checkout directory (if used).

## Steps

1. **Download the latest release:**
   - Go to the [GitHub Releases page](https://github.com/openinfraops/powershell-dsc-pullserver/releases) and download the pre-built executable for your platform.
   - Extract the archive. Make sure to keep the `web/`, `templates/`, and `db/` directories, which are required (the web interface, the HTML templates, and the SQL schema files) and are included in the release zip.
   - Place the executable and all required directories in your desired location.

   **Expected directory structure:**
   ```
   go-dsc-pull/
   ├── DSC-Admin-Console.exe
   ├── config.json
   ├── web/
   ├── templates/
   ├── db/
   ```

2. **Configure the application:**
    - Edit `config.json` for database, SAML, and web UI settings.
   - See example configuration files:
   - For SAML configuration details, see [SAML configuration documentation](webui/authentication.md).
       - [config_sqlite.json](example/config/config_sqlite.json) (SQLite, no SAML)
       - [config_mssql.json](example/config/config_mssql.json) (MSSQL, no SAML)
       - [config_nosaml.json](example/config/config_nosaml.json) (SQLite, SAML disabled)
       - [config_saml.json](example/config/config_saml.json) (MSSQL, SAML enabled)
   - Place your SSL certificate and key files in the specified paths.
   - **Generate the secrets** (`dsc_pullserver.registrationKey` and `web_ui.shared_secret`) with the provided script, then copy them into `config.json`:
     ```sh
     .\scripts\Generate-SecureKeys.ps1
     ```
     The script prints strong values and can emit them as JSON (`-AsJson`) or write them to a file (`-OutputFile .\secret-keys.txt`). Never use a short/guessable secret.

3. **Database setup:**
   - **SQLite:** nothing to do. The server creates the `dsc.db` file and the tables automatically on first start (`db.InitDB`).
   - **MSSQL:** the **database** must exist on your SQL Server instance (`CREATE DATABASE DSCPullServer`). The **tables** are created automatically by the server on start; the [db/schema_mssql.sql](../db/schema_mssql.sql) script is provided as a reference and for pre-provisioning if you prefer to create the schema before first run.
   - The server loads and initializes the schema automatically when the database is empty (check `dsc-pull-server.log` for confirmation). You do **not** need to run the schema script manually on a fresh install.

4. **Create the initial administrator account:**
   - On first run, the server no longer creates a default admin account (`admin@localhost / password`). Initialize the first administrator locally with a strong, randomly generated password:
     ```sh
     ./DSC-Admin-Console.exe init-bootstrap-admin
     ```
   - The command reads `config.json`, initializes the database schema if needed (requires the `db/` directory next to the executable), creates the `admin@localhost` account (role `admin`) **only if no user exists**, and prints a **single-use 30-character random password** on the console.
   - Store this password securely and change it after the first login. Re-running the command **refuses to overwrite** an existing account.

> **Configuration file location.** `config.json` is resolved **next to the executable**, so it is found regardless of the process working directory (the Windows SCM starts the service in `System32`). Override the location with the `--config <path>` flag or the `DSC_CONFIG` environment variable:
>
> ```sh
> ./DSC-Admin-Console.exe --config C:\shared\dsc\config.json
> ./DSC-Admin-Console.exe --config C:\shared\dsc\config.json init-bootstrap-admin
> ```
>
> An absolute `--config` path is used as-is; a relative one is resolved next to the executable.

5. **Deploy the server (install as a Windows service):**
   - The recommended deployment is as a **Windows service** so it starts automatically and runs in the background. The provided script installs the files, the registry, the service and the "Add/Remove Programs" entries:
     ```sh
     .\scripts\install-windows.ps1 -CreateService
     ```
     This registers the service as **`DSC-Console`** (binary `DSC-Admin-Console.exe`).
   - Alternatively, register it manually with `sc.exe`:
     1. Open a command prompt as Administrator.
     2. Run the following command (adapt the path as needed):
        ```sh
        sc.exe create DSC-Console DisplayName= "DSC-Admin-Console" binPath= "C:\\Path\\To\\DSC-Admin-Console.exe" start= auto
        ```
     3. Start the service:
        ```sh
        sc.exe start DSC-Console
        ```
     4. To stop or delete the service:
        ```sh
        sc.exe stop DSC-Console
        sc.exe delete DSC-Console
        ```
   - (Alternative) Run the executable in the foreground for a quick test:
     ```sh
     ./DSC-Admin-Console.exe
     ```
   - See [Windows Deployment Best Practices](#windows-deployment-best-practices) for the least-privilege service account and recommended permissions.

6. **Certificate management:**
   - Use a trusted certificate for production.
   - The example `config.json` files enable **mutual TLS** (`enable_client_cert_validation: true`). When enabled, the DSC endpoint **always** requires a client certificate (mTLS); `bypass_ca_validation` then only controls whether the chain/CA is validated (`true` = accept self-signed node certs and rely on the enrolled thumbprint; `false` = also validate the chain against the OS system trust store). A client certificate requires HTTPS.

7. **Web UI access:**
   - Open your browser and go to `https://your-server:443/web` (or the port specified).

8. **PowerShell module:**
   - Import the module from `powershell/DSCPullServer/`.
   - Authenticate and manage agents, modules, and configurations.

## Troubleshooting
- Check the `dsc-pull-server.log` file (written next to the executable) for errors.
- Ensure all ports are open and certificates are valid.
- Review `config.json` for correct paths and settings.

## Upgrade/Migration
- Backup your database before upgrading.
- Run migration scripts if schema changes are present.
- Restart the server after upgrade.

## Windows Deployment Best Practices

- **Run as a least-privilege service account**, not `LocalSystem`. Grant only the permissions the server needs:
  - read: the executable, `config.json`, the TLS certificate/key, the SAML SP key/cert, and the `web/`, `templates/`, and `db/` (schema) directories;
  - write: the **installation directory** (for the SQLite `dsc.db` file and the `dsc-pull-server.log` file), and the configuration-repository checkout directory (if used).
  - If the account does not have rights to `C:\Windows\System32\...` etc., that is expected — do not widen permissions.
- **Restrict file permissions.** Set `config.json` to `0600` (it holds secrets and the DB password). Protect the TLS private key and the SAML SP key the same way. The server writes `config.json` with `0600` when it persists settings.
- **Use a properly formed HTTPS certificate.** The certificate hostname/SAN must match the hostname or IP the clients use. For the DSC endpoint the certificate is presented to the node LCM; for the web UI it is presented to browsers. The server warns at startup when HTTPS is disabled, and **refuses to start** (fail-closed) if `enable_client_cert_validation` is enabled while the DSC endpoint is served over plain HTTP (see [DSC security](dscpull/security.md)).
- **Open only the needed ports** in the firewall (DSC `8484`, web UI `443`). Do not expose plain HTTP to the network.
- **Enable log rotation** (`enable_log_rotation: true`) and monitor the size of `dsc-pull-server.log` and its rotated backups.
- **Graceful shutdown.** Stopping the Windows service lets in-flight requests finish and the scheduler tasks stop cleanly (the service uses a cancellable context): prefer `sc.exe stop DSC-Console` over killing the process. Avoid `taskkill /F`, which tears down requests mid-flight. On **non-Windows** (Docker, systemd), send **`SIGTERM`** or **`SIGINT`** so the process shuts down gracefully (it drains in-flight requests within the 10 s shutdown window) instead of `SIGKILL`.
- **Upgrade procedure.** Back up the database and `config.json` first. Apply the relevant `db/migration_*.sql` script, replace the executable, then restart the service. Verify schema initialisation in `dsc-pull-server.log`.
- **Back up the database** (and the configuration-repository checkout if used) before any upgrade or maintenance.

## Security Notes
- Always use HTTPS in production.
- Restrict admin access and review logs regularly.
- The initial admin password is generated randomly by `init-bootstrap-admin` (30 characters) and printed **once** on the console. Change it after the first login. The server never creates a default/known admin password automatically.