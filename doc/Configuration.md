# Configuration Reference

This page describes all configuration options available in `config.json` for the DSC Admin Console.

---

## Top-Level Structure

```json
{
  "database": { ... },
  "dsc_pullserver": { ... },
  "web_ui": { ... },
  "saml": { ... }
}
```

---

## `database` (object)

| Key        | Type    | Description                                 | Example                |
|------------|---------|---------------------------------------------|------------------------|
| name       | string  | Database name                               | `"dsc"`                |
| driver     | string  | Database driver (`sqlite` or `mssql`)       | `"mssql"`              |
| server     | string  | Database server address                     | `"localhost"`          |
| user       | string  | Database username                           | `"sa"`                 |
| password   | string  | Database password                           | `"Soleil123!"`         |
| port       | int     | Database port                               | `1433`                 |

---

## `dsc_pullserver` (object)

| Key                        | Type    | Description                                         | Example                |
|----------------------------|---------|-----------------------------------------------------|------------------------|
| cert_file                  | string  | Path to SSL certificate                             | `"dsc-dev.local.crt"`  |
| key_file                   | string  | Path to SSL private key                             | `"dsc-dev.local.key"`  |
| enable_https               | bool    | Enable HTTPS                                        | `true`                 |
| enable_client_cert_validation | bool | Always require a client certificate on the DSC endpoint | `true`                 |
| bypass_ca_validation       | bool    | `true` = accept any client cert (no chain/CA check), identify the node by the enrolled thumbprint only. `false` = also validate the chain against the OS system trust store | `true`                 |
| port                       | int     | API server port                                     | `8484`                 |
| registrationKey            | string  | Key for agent registration (DSC)                    | `"AnyString"`          |
| shared_secret              | string  | JWT signing secret (web/API auth). If both are set, `web_ui.shared_secret` takes precedence. | `"your-strong-secret"` |
| enable_report_auto_cleanup | bool    | Enable periodic deletion of old reports             | `false`                |
| report_retention_days      | int     | Keep reports for this number of days                | `90`                   |
| report_cleanup_interval_mins | int   | Cleanup execution interval in minutes               | `1440`                 |
| enable_release_check       | bool    | Enable periodic GitHub latest release check         | `false`                |
| release_check_interval_mins| int     | Release check execution interval in minutes         | `1440`                 |
| enable_log_rotation        | bool    | Enable log file rotation                            | `true`                 |
| log_rotate_max_size_mb     | int     | Rotate log file when size reaches this value (MB)   | `10`                   |
| log_rotate_max_backups     | int     | Number of rotated log backups to keep               | `5`                    |
| log_rotate_max_age_days    | int     | Delete rotated log backups older than this age       | `30`                   |
| enable_configuration_repo_sync | bool | Enable the configuration repository sync (legacy)  | `false`                |
| configuration_repo_url     | string  | Git repository URL (legacy, better set via UI)       | `""`                   |
| configuration_repo_branch  | string  | Branch to sync (legacy)                             | `"main"`               |
| configuration_repo_directory | string | Local checkout directory (legacy)                  | `"data/configuration-repository"` |
| configuration_compiler_script | string | Compiler script path (legacy)                     | `"scripts/Compile-Configurations.ps1"` |
| configuration_powershell_executable | string | PowerShell executable (legacy)              | `"pwsh"`               |
| configuration_repo_username | string  | Git username (legacy, optional)                     | `""`                   |
| configuration_repo_password | string  | Git password/token (legacy, optional)               | `""`                   |
| configuration_repo_sync_interval_mins | int | Sync interval (legacy)                          | `60`                   |
| enable_agent_purge         | bool    | Enable periodic soft-deleted agent purge             | `false`                |
| agent_purge_retention_days | int     | Keep soft-deleted agents for this many days          | `30`                   |
| agent_purge_interval_mins  | int     | Purge interval in minutes                            | `1440`                 |
| enable_configuration_version_cleanup | bool | Enable old configuration version cleanup       | `false`                |
| configuration_version_max_keep | int  | Max configuration versions to keep                   | `10`                   |
| configuration_version_cleanup_interval_mins | int | Cleanup interval in minutes                  | `1440`                 |

> **Client certificate validation (DSC endpoint).** When `enable_client_cert_validation` is `true`, the DSC endpoint **always** requires a client certificate. `bypass_ca_validation` only controls the CA/chain validation of that certificate: `true` accepts any certificate (self-signed node certs work, the node is identified by its enrolled thumbprint only — classic DSC model); `false` additionally validates the chain/signature against the **OS system trust store** (`RequireAndVerifyClientCert`) and still checks the enrolled thumbprint. A client certificate requires HTTPS; in pure HTTP mode (`enable_https: false`) no certificate is negotiated.

The Git-backed **configuration repository** settings are **stored in the database** (`configuration_repo_settings`) and managed from the **Scheduler administration page** — they are **not** edited in `config.json` (the `configuration_repo_*` keys here are only a legacy migration source imported once). See [Configuration repository (Git) & Scheduler](dscpull/configuration-repo.md) for the full settings reference and the compiler contract.

---

## `web_ui` (object)

| Key        | Type    | Description                                 | Example                |
|------------|---------|---------------------------------------------|------------------------|
| cert_file  | string  | Path to SSL certificate                     | `"dsc-dev.local.crt"`  |
| key_file   | string  | Path to SSL private key                     | `"dsc-dev.local.key"`  |
| enable_https | bool  | Enable HTTPS for web UI                     | `true`                 |
| port       | int     | Web UI port                                 | `443`                  |
| shared_secret | string | JWT signing secret for the web/API. Takes precedence over `dsc_pullserver.shared_secret`. | `"your-strong-secret"` |
| trusted_proxies | []string | IPs/CIDRs of trusted reverse proxies. The `X-Forwarded-For` / `X-Real-IP` headers are only honored for the login rate-limiter when the direct peer (`RemoteAddr`) belongs to this list; otherwise they are ignored (prevents brute-force bypass by spoofing a forward header). | `["10.0.0.0/8", "172.16.0.1"]` |

> **Note:** `web_ui` shares the same configuration structure as `dsc_pullserver` (both use `HTTPConfig`), so it also accepts the certificate/validation/cleanup keys. In practice only `cert_file`, `key_file`, `enable_https`, `port`, `shared_secret` and `trusted_proxies` are meaningful for the web/API server.

---

## `saml` (object)

| Key             | Type    | Description                             | Example                |
|-----------------|---------|-----------------------------------------|------------------------|
| enabled         | bool    | Enable SAML authentication              | `true`                 |
| entity_id       | string  | SAML Service Provider Entity ID         | `"https://dsc-dev.local"` |
| idp_metadata_url| string  | IdP metadata URL                        | `"https://login.microsoftonline.com/.../FederationMetadata.xml"` |
| sp_key_file     | string  | Path to SP private key                  | `"sp.key"`             |
| sp_cert_file    | string  | Path to SP certificate                  | `"sp.crt.pem"`         |
| user_mapping    | object  | SAML attribute mapping (see below)      |                        |
| group_mapping   | object  | SAML group mapping (see below)          |                        |

### `user_mapping` (object)

| Key        | Type    | Description                                 | Example                |
|------------|---------|---------------------------------------------|------------------------|
| email      | string  | SAML attribute for email                    | `"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"` |
| sn         | string  | SAML attribute for surname                  | `"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname"`      |
| givenName  | string  | SAML attribute for given name               | `"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"`    |

### `group_mapping` (object)

| Key         | Type    | Description                                | Example                |
|-------------|---------|--------------------------------------------|------------------------|
| attribute   | string  | SAML attribute for group                   | `"http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"` |
| admin_value | string  | Value for admin group                      | `"e25f7f74-df75-448f-9685-71fd21d35f07"` |
| user_value  | string  | Value for user group                       | `"b79fbf4d-3ef9-4689-8143-76b194e85509"` |

---

## Example `config.json`

```json
{
  "database": {
    "name": "dsc",
    "driver": "mssql",
    "server": "localhost",
    "user": "sa",
    "password": "Soleil123!",
    "port": 1433
  },
  "dsc_pullserver": {
    "cert_file": "dsc-dev.local.crt",
    "enable_https": true,
    "enable_client_cert_validation": true,
    "bypass_ca_validation": true,
    "enable_report_auto_cleanup": false,
    "enable_release_check": false,
    "key_file": "dsc-dev.local.key",
    "port": 8484,
    "registrationKey": "AnyString",
    "release_check_interval_mins": 1440,
    "enable_log_rotation": true,
    "log_rotate_max_size_mb": 10,
    "log_rotate_max_backups": 5,
    "log_rotate_max_age_days": 30,
    "report_retention_days": 90,
    "report_cleanup_interval_mins": 1440,
    "shared_secret": "pQw8vZ3rT6sJkL2xQ9eBfG7hN1uVtX4yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1",
  },
  "saml": {
    "enabled": true,
    "entity_id": "https://dsc-dev.local",
    "idp_metadata_url": "https://login.microsoftonline.com/acee6c11-4aea-4377-8960-8261756492fd/FederationMetadata/2007-06/FederationMetadata.xml?appid=57a31120-f9fb-468f-b078-8c0adbbcd162",
    "sp_key_file": "sp.key",
    "sp_cert_file": "sp.crt.pem",
    "user_mapping": {
      "email": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
      "sn": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
      "givenName": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
    },
    "group_mapping": {
      "attribute": "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
      "admin_value": "e25f7f74-df75-448f-9685-71fd21d35f07",
      "user_value": "b79fbf4d-3ef9-4689-8143-76b194e85509"
    }
  },
  "web_ui": {
    "cert_file": "dsc-dev.local.crt",
    "enable_https": true,
    "key_file": "dsc-dev.local.key",
    "shared_secret": "pQw8vZ3rT6sJkL2xQ9eBfG7hN1uVtX4yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1",
    "port": 443
  }
}
```

---

> For any changes, restart the server to apply new configuration values.
