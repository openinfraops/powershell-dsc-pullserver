# Forwarding logs and audit events to a syslog server

The DSC Admin Console writes its application logs to a local file
(`dsc-pull-server.log`, next to the executable) with an optional rotation
worker. In production, operators often need to centralise these logs in a SIEM,
Graylog, rsyslog or Zabbix so they survive a server compromise or disk loss and
can be correlated across systems.

This page describes the optional **syslog output**: it forwards application log
records and audit events to a remote syslog server (RFC 5424) **in addition to**
the local file. It is disabled by default, so existing installations are
unaffected.

---

## Configuration

Add a `syslog` section to `config.json`:

```json
{
  "syslog": {
    "enabled": false,
    "network": "tcp+tls",
    "address": "syslog.example.local:6514",
    "facility": "local0",
    "app_name": "dsc-pull-server",
    "min_level": "info",
    "ca_file": "",
    "client_cert_file": "",
    "client_key_file": "",
    "insecure_skip_verify": false
  }
}
```

| Key                   | Type   | Default           | Description                                                                                |
|-----------------------|--------|-------------------|--------------------------------------------------------------------------------------------|
| `enabled`             | bool   | `false`           | Enable the syslog output.                                                                  |
| `network`             | string | `"tcp+tls"`       | Transport: `udp`, `tcp` or `tcp+tls` (TLS, RFC 5425 — recommended).                        |
| `address`             | string | —                 | Remote syslog server `host:port` (required when enabled).                                  |
| `facility`            | string | `"local0"`        | Syslog facility: `local0`..`local7`, `auth`, `authpriv`, `daemon`, `syslog`, `user`, etc.   |
| `app_name`            | string | `"dsc-pull-server"`| RFC 5424 `APP-NAME` (max 48 chars, spaces removed).                                        |
| `min_level`           | string | `"info"`          | Minimum level forwarded: `debug`, `info`, `warning`, `error`, `critical`.                  |
| `ca_file`             | string | `""`              | PEM CA bundle used to verify the syslog server in `tcp+tls`. Empty → system trust store.   |
| `client_cert_file`    | string | `""`              | Client certificate for mutual TLS (`tcp+tls`). Must be set with `client_key_file`.        |
| `client_key_file`     | string | `""`              | Client private key for mutual TLS (`tcp+tls`).                                             |
| `insecure_skip_verify`| bool   | `false`           | Disable server certificate verification (`tcp+tls`). Triggers a startup WARN.             |

> The configuration is validated at startup. An invalid `network`, a missing
> `address`, an unknown `facility`/`min_level`, a malformed `ca_file` or an
> incomplete client-cert pair **refuses to start** and logs a clear error.

---

## Transports

- **`udp`** — RFC 5426. Unencrypted and unreliable; a dropped datagram is never
  retried. A startup WARN is logged because the traffic is not encrypted.
- **`tcp`** — RFC 6587 framing (newline-delimited). Reliable, still unencrypted.
- **`tcp+tls`** — RFC 5425. Recommended. Server certificate is verified against
  `ca_file` (or the system trust store). Mutual TLS is supported via
  `client_cert_file`/`client_key_file`.

---

## Level mapping

The application embeds the level as a prefix of each log message (`INFO`,
`ERROR`, `WARN`, `ALERT`, or `[ERROR] …`). It is mapped to an RFC 5424
severity:

| Application level | Syslog severity | PRI suffix |
|-------------------|-----------------|------------|
| `DEBUG`           | debug (7)       | `…7`       |
| `INFO`, `NOTICE`  | info (6)        | `…6`       |
| `WARN`, `WARNING` | warning (4)     | `…4`       |
| `ERROR`, `ERR`    | error (3)       | `…3`       |
| `ALERT`, `CRITICAL`, `FATAL`, `EMERG` | critical (2) | `…2` |

`min_level` filters the forwarded records: `info` forwards everything through
`info` (the default), `warning` only `warning` and above, and so on.

---

## Forwards

Each forwarded message is an RFC 5424 record:

```
<PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
```

- **Application logs** use the configured `app_name`, a `-` `MSGID` and no
  structured data.
- **Audit events** are emitted as a **distinct, filterable stream**: they use
  `MSGID = AUDIT` and a structured-data element `[audit@48577 user=…
  action=… target=… ip=…]`. Audit events ignore `min_level` — they are always
  forwarded when syslog is enabled.

---

## Behaviour and resilience

- **Non-blocking.** A slow or unreachable syslog server never blocks requests or
  the DSC pull cycle. Records are placed in a bounded in-memory queue
  (10 000 entries) and sent by a background worker.
- **Drop-oldest.** If the queue is full, the oldest queued record is discarded,
  a drop counter is incremented and a throttled `WARN [SYSLOG]` is written to
  the local file.
- **Reconnection with backoff.** The worker reconnects automatically with an
  exponential backoff (500 ms → 30 s).
- **Local file keeps working.** The local `dsc-pull-server.log` is always
  written, whether or not syslog succeeds.
- **Windows service.** The syslog output uses only standard network/TLS primitives
  — no dependency on the console, the interactive session or the Windows Event
  Log.

---

## Security

The forwarded **message text is the same** that is written to the local log file
(which the application already keeps free of secrets such as the registration
key, JWT secret, MOF contents or passwords). Control characters are stripped to
prevent log-line injection. Audit structured-data fields are escaped to keep the
record valid RFC 5424.

- `insecure_skip_verify: true` is honored only when explicitly set, and logs a
  **startup WARN** (TLS verification disabled).
- Using `udp` also logs a **startup WARN** (unencrypted, unreliable).

---

## rsyslog receiver example

Receiving DSC logs over TLS (6514) and routing the audit stream to a dedicated
file:

```conf
# /etc/rsyslog.d/dsc.conf
module(load="imtcp" MaxSessions="500")
module(load="imtls")

input(type="imtcp" port="6514" tls.cacert="/etc/rsyslog/ca.pem" tls.caCert="..")

# DSC application logs -> central file
if $app-name == "dsc-pull-server" and $msgid != "AUDIT" then /var/log/dsc/dsc-app.log
& stop

# DSC audit events -> dedicated audit file (filterable)
if $app-name == "dsc-pull-server" and $msgid == "AUDIT" then /var/log/dsc/dsc-audit.log
& stop
```

> Enable the relevant rsyslog TLS module and create the target directories
> (`/var/log/dsc/`) with the correct ownership/permissions (often
> `syslog:adm`). Adjust the CA certificate to the one the DSC server uses for
> verification (`ca_file`).

---

## Zabbix example

Zabbix does not ingest syslog natively, but you can push the DSC log severity
into Zabbix from a host that receives the stream. Two common options:

### Option 1 — Zabbix agent log items on the rsyslog file

On the host running rsyslog, configure a Zabbix **agent** log item pointing at
the per-app file, then trigger on error severity:

```text
# Item: DSC errors
Type:          Zabbix agent (passive)
Key:           log[/var/log/dsc/dsc-app.log,"<134>1",,,skip]
Type of info:  Log
Update interval: 10s
```

```text
# Trigger: DSC syslog error
Trigger:       find(/DSC syslog/DSCC errors today,,"like","ERROR") > 0
```

### Option 2 — forward to a SIEM/syslog bridge

Point the syslog output at an aggregation endpoint (rsyslog, Graylog, logstash)
and let that endpoint forward to Zabbix via `zabbix_sender`, or ingest the audit
stream (`[audit@48577 …]`) into a security analytics platform.

---

## See also

- [Configuration reference](Configuration.md) for the full `config.json` guide.
- [DSC Pull Server configuration](dscpull/configuration.md).
