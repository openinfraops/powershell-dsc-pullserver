# Node Registration

This page explains how to register nodes (agents) with the DSC Pull Server.

## Registration Process

1. **Obtain the registration key:**
   - The registration key is defined in the `config.json` file under the `dsc_pullserver` section (option: `registrationKey`).

   > You can pre-register an agent using the web interface or API before configuring the LCM on the agent side. This allows you to add tags, link a configuration in advance, or set other metadata for the agent.

2. **Agent configuration:**
   - Configure your DSC agent to use the server URL and provide the registration key during registration.
      - Example agent configuration (LCM) for DSC Pull:
      
         The Local Configuration Manager (LCM) on Windows nodes is configured via a MOF file or PowerShell script. Here is a sample configuration for DSC Pull with reporting enabled:
      
         ```powershell
         [DSCLocalConfigurationManager()]
         Configuration LCMConfig {
            Node "localhost" {
               Settings {
                  RefreshMode = "Pull"
                  RefreshFrequencyMins = 30
                  ConfigurationMode = "ApplyAndMonitor"
                  ConfigurationModeFrequencyMins = 15
                  RebootNodeIfNeeded = $true
               }
               ConfigurationRepositoryWeb PullSrv {
                  ServerURL = "https://your-server:8484/PSDSCPullServer.svc"
                  RegistrationKey = "AnyString"
               }
               ReportServerWeb PullReport {
                  ServerURL = "https://your-server:8484/PSDSCPullServer.svc"
                  RegistrationKey = "AnyString"
               }
            }
         }
         LCMConfig
         Set-DscLocalConfigurationManager -Path ./LCMConfig
         ```
      
- `ReportServerWeb`: URL for reporting to the DSC Pull Server.
    > Note: For now, the path must include `PSDSCPullServer.svc` (the official Microsoft service URL is retained). This requirement may be revisited in the future to allow custom endpoints.
- `DownloadManagerCustomData`: Contains the server URL and registration key.
- `RefreshMode`: Set to "Pull" for DSC Pull mode.
- `RefreshFrequencyMins`: How often the node checks for new configurations.
- `ConfigurationMode`: "ApplyAndMonitor" enables reporting.
- `ConfigurationModeFrequencyMins`: How often the node applies and monitors configuration.
- `RebootNodeIfNeeded`: Allows automatic reboot if required.
      
             Apply this configuration using PowerShell to set up the LCM for DSC Pull and reporting.

### Example: Configuration with ConfigurationRepositoryWeb and ReportServerWeb

Here is a typical LCM configuration using ConfigurationRepositoryWeb and ReportServerWeb blocks:

```powershell
[DSCLocalConfigurationManager()]
Configuration PullLCMConfig {
   Node "localhost" {
      Settings {
         RefreshMode = "Pull"
         ConfigurationMode = "ApplyOnly"
      }
      ConfigurationRepositoryWeb PullSrv {
         ServerURL = "http://127.0.0.1:8080/PSDSCPullServer.svc"
         AllowUnsecureConnection = $true
         RegistrationKey = "Test"
      }
      ReportServerWeb PullReport {
         ServerURL = "http://127.0.0.1:8080/PSDSCPullServer.svc"
         AllowUnsecureConnection = $true
      }
   }
}
PullLCMConfig
Set-DscLocalConfigurationManager -Path ./PullLCMConfig
```

- `ConfigurationRepositoryWeb`: Defines the pull server endpoint and registration key for configuration retrieval.
- `ReportServerWeb`: Defines the endpoint for reporting compliance and status.
- `AllowUnsecureConnection`: Set to `$true` to allow HTTP (not recommended for production).
- `ServerURL`: Must include `PSDSCPullServer.svc`.
- `RegistrationKey`: Must match the server configuration.

> Note: Using HTTP is only suitable for testing or isolated environments. Always use HTTPS in production for security.

3. **Certificate requirements:**
- Client certificate validation is controlled by `enable_client_cert_validation` in `config.json` (`dsc_pullserver` section). When `true`, the DSC endpoint **always** requires a client certificate.
- `bypass_ca_validation` only controls the **CA/chain validation** of that certificate, not whether it is required:
  - `bypass_ca_validation: true` → any certificate is accepted at the TLS layer; the node is identified **only** by its **enrolled thumbprint** (self-signed node certificates work). Classic DSC model.
  - `bypass_ca_validation: false` → the certificate chain/signature is additionally validated against the **OS system trust store** (`RequireAndVerifyClientCert`), and the enrolled thumbprint is still checked. No extra config field is needed.
- HTTPS is required to present a client certificate. In pure HTTP mode (`enable_https: false`) no certificate is negotiated, so the endpoints are unauthenticated at the transport level — only use for testing/isolated networks.

> ⚠️ **`bypass_ca_validation: false` also applies to the registration endpoint.**
> The `RequireAndVerifyClientCert` policy is set at the **whole DSC listener** level (`main.go`), including the registration route `PUT /PSDSCPullServer.svc/{node}`. In this mode the TLS handshake happens **before** the registration-key (HMAC) step, so **a node that presents a self-signed certificate cannot register at all** — the TLS handshake is rejected. To use `bypass_ca_validation: false` in production, every node must present a certificate issued by a CA that is trusted by the server's OS trust store (e.g. your internal PKI). If you rely on self-signed node certificates, keep `bypass_ca_validation: true`.

#### Certificate renewal / rollover

The node's certificate information (thumbprint, subject, issuer, `notBefore`/`notAfter`) is captured **at registration** from `RegistrationInformation.CertificateInformation` and stored in the `agents` table. When a node's certificate is renewed, the node **re-registers** with the pull server (same registration endpoint, same registration key): the stored certificate information is refreshed and the new thumbprint becomes the one checked by the middleware.

> **Backend behaviour on re-registration (both SQLite and MSSQL):**
> - A **new** agent (first enrollment) is **inserted** with `state = pending_apply`.
> - An **already-registered** agent (e.g. certificate renewal) is **updated in place**: `node_name`, `lcm_version`, `registration_type` and the certificate fields (`certificate_thumbprint`, `certificate_subject`, `certificate_issuer`, `certificate_notbefore`, `certificate_notafter`) are refreshed, while `state`, `last_communication`, `has_error_last_report`, `needs_recompile` and `registered_at` are preserved.

4. **Successful registration:**
- Upon successful registration, the agent will appear in the web interface and can receive configurations.

## Troubleshooting

- Check server logs for registration errors.
- Ensure the registration key matches the value in config.json.
- Verify network connectivity and certificate validity.

---

> For advanced scenarios (SAML, JWT, custom roles), see the authentication and security documentation.
