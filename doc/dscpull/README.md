# DSC Pull Server Documentation

This section details the operation, installation, and security of the DSC Pull Server.

## Table of Contents
- [Configuration](configuration.md)
    - Edit the `config.json` file to set up the database, web access, SAML/JWT authentication, and certificate paths according to your environment. For the **exhaustive per-key reference** of every section (`database`, `dsc_pullserver`, `web_ui`, `saml`, `syslog`), see [Configuration reference](../Configuration.md).
- [Forwarding logs to a syslog server](../syslog.md) — optional RFC 5424 syslog output (UDP/TCP/TCP+TLS) with rsyslog and Zabbix examples.
- [Configuration repository (Git) & Scheduler](configuration-repo.md)
- [Node Registration](registration.md)
- [Security](security.md)
- [MS-DSCPM protocol implementation state](protocol-implementation.md)