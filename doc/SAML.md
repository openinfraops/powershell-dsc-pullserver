# SAML Integration Reference

This page documents the SAML authentication integration for the DSC Pull Server. It explains configuration, user/group mapping, endpoints, and troubleshooting for SAML-based Single Sign-On (SSO).

---

## Overview

SAML (Security Assertion Markup Language) enables Single Sign-On (SSO) with external Identity Providers (IdP) such as Azure AD, ADFS, or other SAML-compliant services. The DSC Pull Server can act as a SAML Service Provider (SP).

---

## Configuration

SAML settings are defined in `config.json` under the `saml` section:

```json
"saml": {
  "enabled": true,
  "entity_id": "https://dsc-dev.local",
  "idp_metadata_url": "https://login.microsoftonline.com/.../FederationMetadata.xml",
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
}
```

- **enabled**: Enable/disable SAML authentication.
- **entity_id**: The SP Entity ID (must match IdP configuration).
- **idp_metadata_url**: URL to the IdP metadata XML.
- **sp_key_file**: Path to the SP private key.
- **sp_cert_file**: Path to the SP certificate.
- **user_mapping**: Maps SAML attributes to user fields.
- **group_mapping**: Maps SAML group claims to roles.

---

## Endpoints

- **GET** `/saml/login` — Redirects to the IdP for authentication.
- **POST** `/saml/acs` — Assertion Consumer Service (receives the SAML response).
- **GET** `/saml/metadata` — SP metadata.
- **GET** `/api/v1/saml/userinfo` — Returns SAML user attributes for the current session.
- **GET** `/api/v1/saml/enabled` — Returns whether SAML is enabled.
- **GET** `/api/v1/saml/config` — Get the SAML configuration (admin).
- **PUT** `/api/v1/saml/config` — Update the SAML configuration (admin).
- **POST** `/api/v1/saml/upload_sp_keycert` — Upload the SP key/certificate (admin).

---

## User & Group Mapping

- **user_mapping**: Defines which SAML attributes are used for email, surname, and given name.
- **group_mapping**: Determines admin/user roles based on SAML group values.
  - `attribute`: SAML group claim attribute.
  - `admin_value`: Value that grants admin rights.
  - `user_value`: Value that grants user rights.

---

## Example SAML Response (Excerpt)

```xml
<AttributeStatement>
  <Attribute Name="http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress">
    <AttributeValue>user@example.com</AttributeValue>
  </Attribute>
  <Attribute Name="http://schemas.microsoft.com/ws/2008/06/identity/claims/groups">
    <AttributeValue>e25f7f74-df75-448f-9685-71fd21d35f07</AttributeValue>
  </Attribute>
</AttributeStatement>
```

---

## Troubleshooting

- Ensure SP certificate and key files are present and valid.
- The `entity_id` must match the IdP configuration.
- The IdP metadata URL must be accessible by the server.
- Check server logs for SAML errors or misconfigurations.
- User/group mapping must match the IdP's SAML attribute names and values.

---

## Security Notes

- SAML authentication is only as secure as your IdP and SP key/cert management.
- Always use HTTPS for SAML endpoints.
- Rotate SP keys/certs periodically.

---

> For more details, see the SAML configuration in `config.json` and the handler code in `handlers/saml_*.go`.
