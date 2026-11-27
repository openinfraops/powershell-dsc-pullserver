# Release v1.9.1

Date: 2026-11-27

Patch release fixing two issues around the v1.9.0 line: a Zabbix 7.0
monitoring template that failed to import, and a registration edge case that
could leave an orphaned configuration link and fail on MSSQL.

## API changes

**No API changes.** The `/api/v1/*` endpoints keep the same contract. This
release only fixes a registration edge case (a failed agent upsert no longer
attempts to assign a configuration) and corrects the Zabbix template.

## Changes

### Fix: Zabbix 7.0 template import failed

The monitoring template placed its `triggers` block at the template level,
which Zabbix 7.0 rejects (`Invalid tag "/zabbix_export/templates/template(1)":
unexpected tag "triggers"`). The importer (`getTriggers` in
`CImportDataAdapter.php`) reads triggers only from `templates[].items[].triggers`
or `zabbix_export.triggers`, never `templates[].triggers`.

All 11 triggers were moved into their corresponding items (item-level
`triggers`), matching the format of the official Zabbix community templates.
`monitoring/zabbix_template.yml` now imports cleanly into Zabbix 7.0.

### Fix: registration no longer assigns a config link for an uncreated agent

On MSSQL, a new-node registration whose agent upsert (or TEMP migration) failed
still inserted the default node-name link into `agent_configurations` for an
`agent_id` that does not exist in `agents`, violating the foreign key
`FK__agent_con__agent__0B91BA14` and logging:

```
[ERROR][REGISTER][CONFIG] Failed to assign node configuration <node>:
... conflict ... 'agent_id'
```

The config-link replacement, the default config assignment and the async
recompile are now gated on a new `agentOK` flag that is `true` only when the
agent row was actually created (upsert) or migrated (TEMP). This prevents the
dangling `agent_id` and the resulting foreign-key error.

## Impacted files

- `handlers/register.go`
- `handlers/dsc_integration_test.go`
- `monitoring/zabbix_template.yml`
- `db/schema_sqlite.sql`, `db/schema_mssql.sql`
- `build/version.json`
- `.github/workflows/release.yml`
- `doc/dscpull/configuration.md`
- `SECURITY.md`, `doc/Integrations.md`, `doc/README.md`
- `doc/release_v1.9.1.md`
