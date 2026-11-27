# Security Policy

This project is the **DSC Pull Server** (`powershell-dsc-pullserver`): a Go-based
Desired State Configuration pull server, a PowerShell module and a web
administration UI. The security model and hardening guidance (authentication,
client-certificate validation, certificate lifecycle & renewal, rate limiting,
startup warnings) are documented in
[doc/dscpull/security.md](doc/dscpull/security.md).

## Supported versions

Security fixes are applied to the **latest release** of the project. The
currently supported release is **`v1.9.0`**. Older releases are **not**
actively patched — please upgrade to the latest release to receive security
updates.

| Version | Supported |
|---|---|
| `v1.9.0` (latest) | ✅ |
| `v1.8.x` and earlier | ❌ (please upgrade) |

## Reporting a vulnerability

We take security issues seriously. Please **report privately** and **do not
disclose** a suspected vulnerability publicly (no public issue, pull request,
forum, or social-media post) before a fix is available.

**Preferred channel: a GitHub Security Advisory** (private, report-only) on
this repository:

1. Open the repository **Security** tab.
2. Click **Report a vulnerability** (or **Advisories → New advisory**).
3. Fill in the details below. The draft stays private until you publish it.

Alternatively, contact the maintainer directly on GitHub: [@gerardlemetayerc](https://github.com/gerardlemetayerc).

## What to include

- **Affected version / commit** — the release tag (e.g. `v1.7.2`) or the
  `git describe` output of the build (`DSC-Admin-Console.exe --version` / the
  about page).
- **Component** — DSC endpoint (`/PSDSCPullServer.svc/...`), web UI / REST API,
  PowerShell module, SAML, or database schema.
- **Description** — the vulnerability and its security impact
  (confidentiality / integrity / availability).
- **Steps to reproduce** — the exact request(s) and relevant configuration
  (redact secrets in the `config.json` excerpt).
- **Proof of concept** — payload, script, or capture, where possible.
- **Severity** — your CVSS v3/v4 assessment, if you have one.

## Scope

**In scope:**

- Authentication bypass or certificate-binding bypass on the DSC pull endpoints.
- Authorization / privilege-escalation issues in the web API or UI.
- SAML or JWT validation weaknesses.
- Disclosure of secrets (registration key, `shared_secret`, configuration MOFs)
  or node certificate private material.
- Denial of service on the DSC endpoints (memory exhaustion, request-body limits).

**Out of scope** (please do not report): missing security headers, weak TLS
cipher suites in a default deployment, or issues that already require an
authenticated administrator.

## Response timeline

- **Acknowledgment** — within **3 business days** of a valid report.
- **Initial triage** — within **5 business days** (severity assessment).
- **Fix & release** — as fast as possible depending on severity:
  - **Critical / High**: a fix and release as soon as possible, typically within days.
  - **Medium / Low**: a fix in the next planned release.

## Coordinated disclosure

We follow **coordinated disclosure**. We will: acknowledge the report,
reproduce and confirm the issue, work with you on the fix and a security
advisory, credit you (unless you prefer to stay anonymous), and publish the
advisory plus a patched release. Please give us a reasonable time to fix and
release before publishing anything; if you believe we are not responding, we
ask that you contact us first so we can set an expectation.
