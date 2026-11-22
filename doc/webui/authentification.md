# Authentication

This page explains the authentication mechanisms available in the DSC Pull Server web interface.

## Supported Methods
- Local authentication (username/password)
- SAML Single Sign-On (SSO)

> **First local admin:** the server never creates a default admin account. Create the first administrator locally, once, with:
> ```
> DSC-Admin-Console.exe init-bootstrap-admin
> ```
> This generates a single-use 30-character random password and prints it on the console. See [Security](../dscpull/securite.md) for details.

## SAML Configuration

SAML Single Sign-On (SSO) allows integration with enterprise identity providers (IdP) for centralized authentication.

#### Field Descriptions

- `email`: SAML attribute for user email.
- `sn`: SAML attribute for surname (last name).
- `givenName`: SAML attribute for given name (first name).
- `group_mapping::attribute`: SAML attribute containing group or role attributes value(s).
- `admin_value`: Value identifying admin group(s).
- `user_value`: Value identifying user group(s).

**Important:** SAML role mapping (from group_mapping) always takes precedence over roles configured directly in the application. If a user is mapped as admin via SAML, they will have admin rights regardless of local settings.

- `sp_key_file`: Path to the Service Provider's private key file. This key is used to sign SAML requests and responses, ensuring their authenticity.
- `sp_cert_file`: Path to the Service Provider's public certificate file. This certificate is used by the Identity Provider to verify the signature of SAML requests and responses.

### Setup Steps

1. Obtain the SAML metadata URL (XML) from your IdP administrator (the application must have access to it).
2. Configure the Service Provider (SP) settings: entity ID, ACS URL, certificate, and private key.
3. Map SAML groups/roles to application roles (admin/user) in the configuration file.

### User Mapping

- SAML attributes (such as email, group, role) are mapped to local user accounts using the `user_mapping` section.
- Admins can define mapping rules in config.json for automatic role assignment.
- If a SAML user does not exist locally, an account can be created automatically (if enabled in the application settings).

### Security Notes

- All SAML login attempts and errors are logged for auditing.

### Example Configuration for Entra (config.json)

```json
{
    "saml": {
        "enabled": true,
        "entity_id": "https://dsc-dev.local",
        "idp_metadata_url": "https://login.microsoftonline.com/yourtenantID/FederationMetadata/2007-06/FederationMetadata.xml?appid=appId",
        "sp_key_file": "sp.key",
        "sp_cert_file": "sp.crt.pem",
        "user_mapping": {
            "email": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
            "sn": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
            "givenName": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
        },
        "group_mapping": {
            "attribute": "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
            "admin_value": "9f59531c-ae20-42b6-be94-a4dae3623eb0",
            "user_value": "b79fbf4d-3ef9-4689-8143-76b194e85509"
        }
    }
}
```
**Configuration on application side**

![SAML Configuration Example](../img/saml_config.jpg)

**Configuration on azure side**

![Entra Configuration Example](../img/entra_conf_example.jpg)

**Note:** This configuration can be managed either via the web interface (admin only) or by editing the config file directly. The web UI updates the config file in place.

