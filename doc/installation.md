# Installation Guide

This page explains how to install and deploy the Go DSC Pull Server.


## Steps

1. **Download the latest release:**
   - Go to the [GitHub Releases page](https://github.com/openinfraops/powershell-dsc-pullserver/releases) and download the pre-built executable for your platform.
   - Extract the archive. Make sure to keep the `web/` and `templates/` directories, which are required for the web interface and are included in the release zip.
   - Place the executable and all required directories in your desired location.

   **Expected directory structure:**
   ```
   go-dsc-pull/
   ├── dsc-pull-server.exe
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
     ./dsc-pull-server.exe init-bootstrap-admin
     ```
   - The command reads `config.json`, initializes the database schema if needed (requires the `db/` directory next to the executable), creates the `admin@localhost` account (role `admin`) **only if no user exists**, and prints a **single-use 30-character random password** on the console.
   - Store this password securely and change it after the first login. Re-running the command **refuses to overwrite** an existing account.

4. **Deploy the server:**
    - Run the executable:
       ```sh
       ./dsc-pull-server.exe
       ```
    - (Optional) Register as a Windows service using `sc.exe`:
      1. Open a command prompt as Administrator.
      2. Run the following command (adapt the path as needed):
         ```sh
         sc.exe create DSCPullServer binPath= "C:\\Path\\To\\dsc-pull-server.exe" start= auto
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
   - You can disable client certificate validation in `config.json` for testing.

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

## Security Notes
- Always use HTTPS in production.
- Restrict admin access and review logs regularly.
- The initial admin password is generated randomly by `init-bootstrap-admin` (30 characters) and printed **once** on the console. Change it after the first login. The server never creates a default/known admin password automatically.