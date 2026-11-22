# Installation Guide

This page explains how to install and deploy the Go DSC Pull Server.

## Prerequisites

The server is a single self-contained **Go** executable (no Go runtime required on the target host). Before installing, ensure:

- **Operating system:** Windows (the DSC config-repository **compilation** feature requires Windows + PowerShell 7). The `init-bootstrap-admin` CLI and the web UI are cross-platform, but full DSC pull + compilation are Windows-oriented.
- **Database:** either **Microsoft SQL Server** (MSSQL) or **SQLite** (embedded). Choose the driver in `config.json` → `database.driver`. An existing MSSQL database may require a migration script when upgrading (see `db/migration_*.sql`).
- **PowerShell 7 (`pwsh`)** — **only required** if you use the Git-backed **configuration repository** (compilation). Must be installed on the server and reachable by the service account. The DSC resource modules referenced by your configurations must be installed for that `pwsh` environment.
- **Certificates:**
  - A **server certificate + private key** (PEM) for HTTPS, configured via `cert_file` / `key_file` (DSC and/or web UI). Use a certificate trusted by the clients.
  - For **client-certificate validation** (`enable_client_cert_validation: true`), each DSC node presents its own client certificate; see [DSC security](dscpull/securite.md).
- **Registration key** (`dsc_pullserver.registrationKey`) — shared secret used by DSC nodes to register.
- **JWT signing secret** (`web_ui.shared_secret`, or `dsc_pullserver.shared_secret` for backward compatibility).
- **Network / ports:**
  - DSC pull server port (default `8484`).
  - Web UI / API port (default `443`).
  - Open these in the firewall. In HTTPS mode bind the ports to a trusted certificate.
- **Service account** (when installing as a Windows service): a least-privilege account is recommended (see [Best practices](#windows-deployment-best-practices)). It needs read access to the executable/config/certificates and write access to the `db/`, `logs/`, and configuration-repository checkout directories.

## Steps

1. **Download the latest release:**
   - Go to the [GitHub Releases page](https://github.com/openinfraops/powershell-dsc-pullserver/releases) and download the pre-built executable for your platform.
   - Extract the archive. Make sure to keep the `web/` and `templates/` directories, which are required for the web interface and are included in the release zip.
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
   - For SAML configuration details, see [SAML configuration documentation](webui/authentification.md).
       - [config_sqlite.json](example/config/config_sqlite.json) (SQLite, no SAML)
       - [config_mssql.json](example/config/config_mssql.json) (MSSQL, no SAML)
       - [config_nosaml.json](example/config/config_nosaml.json) (SQLite, SAML disabled)
       - [config_saml.json](example/config/config_saml.json) (MSSQL, SAML enabled)
   - Place your SSL certificate and key files in the specified paths.

3. **Create the initial administrator account:**
   - On first run, the server no longer creates a default admin account (`admin@localhost / password`). Initialize the first administrator locally with a strong, randomly generated password:
     ```sh
     ./DSC-Admin-Console.exe init-bootstrap-admin
     ```
   - The command reads `config.json`, initializes the database schema if needed (requires the `db/` directory next to the executable), creates the `admin@localhost` account (role `admin`) **only if no user exists**, and prints a **single-use 30-character random password** on the console.
   - Store this password securely and change it after the first login. Re-running the command **refuses to overwrite** an existing account.

4. **Deploy the server:**
    - Run the executable:
       ```sh
       ./DSC-Admin-Console.exe
       ```
    - (Optional) Register as a Windows service using `sc.exe`:
      1. Open a command prompt as Administrator.
      2. Run the following command (adapt the path as needed):
         ```sh
         sc.exe create DSCPullServer binPath= "C:\\Path\\To\\DSC-Admin-Console.exe" start= auto
         ```
      3. Start the service:
         ```sh
         sc.exe start DSCPullServer
         ```
      4. To stop or delete the service:
         ```sh
         sc.exe stop DSCPullServer
         sc.exe delete DSCPullServer
         ```

5. **Database setup:**
   - For MSSQL: run `db/schema_mssql.sql` to create tables.
   - For SQLite: run `db/schema_sqlite.sql`.
   - The server can also automatically load and initialize the database schema on start if the database is empty (check logs for confirmation).



6. **Certificate management:**
   - Use a trusted certificate for production.
   - Client certificate validation is disabled by default in `config.json` for testing. When you enable `enable_client_cert_validation`, the DSC endpoint **always** requires a client certificate; `bypass_ca_validation` then only controls whether the chain/CA is validated (`true` = accept self-signed node certs and rely on the enrolled thumbprint; `false` = also validate the chain against the OS system trust store). A client certificate requires HTTPS.

7. **Web UI access:**
   - Open your browser and go to `https://your-server:443/web` (or the port specified).

8. **PowerShell module:**
   - Import the module from `powershell/DSCPullServer/`.
   - Authenticate and manage agents, modules, and configurations.

## Troubleshooting
- Check logs for errors in the `logs/` directory.
- Ensure all ports are open and certificates are valid.
- Review `config.json` for correct paths and settings.

## Upgrade/Migration
- Backup your database before upgrading.
- Run migration scripts if schema changes are present.
- Restart the server after upgrade.

## Windows Deployment Best Practices

- **Run as a least-privilege service account**, not `LocalSystem`. Grant only the permissions the server needs:
  - read: the executable, `config.json`, the TLS certificate/key, the SAML SP key/cert;
  - write: the `db/` directory (SQLite), `logs/`, and the configuration-repository checkout directory.
  - If the account does not have rights to `C:\Windows\System32\...` etc., that is expected — do not widen permissions.
- **Restrict file permissions.** Set `config.json` to `0600` (it holds secrets and the DB password). Protect the TLS private key and the SAML SP key the same way. The server writes `config.json` with `0600` when it persists settings.
- **Use a properly formed HTTPS certificate.** The certificate hostname/SAN must match the hostname or IP the clients use. For the DSC endpoint the certificate is presented to the node LCM; for the web UI it is presented to browsers. Add a `WARN` check: the server logs at startup when HTTPS is disabled or client-certificate validation is enabled over plain HTTP.
- **Open only the needed ports** in the firewall (DSC `8484`, web UI `443`). Do not expose plain HTTP to the network.
- **Enable log rotation** (`enable_log_rotation: true`) and monitor the `logs/` directory size.
- **Graceful shutdown.** Stopping the Windows service lets in-flight requests finish and the scheduler tasks stop cleanly (the service uses a cancellable context): prefer `sc.exe stop DSCPullServer` over killing the process. Avoid `taskkill /F`, which tears down requests mid-flight.
- **Upgrade procedure.** Back up the database and `config.json` first. Apply the relevant `db/migration_*.sql` script, replace the executable, then restart the service. Verify schema initialisation in the logs.
- **Back up the database** (and the configuration-repository checkout if used) before any upgrade or maintenance.

## Security Notes
- Always use HTTPS in production.
- Restrict admin access and review logs regularly.
- The initial admin password is generated randomly by `init-bootstrap-admin` (30 characters) and printed **once** on the console. Change it after the first login. The server never creates a default/known admin password automatically.