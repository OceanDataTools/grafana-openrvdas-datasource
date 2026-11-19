# Changelog

All notable user-facing changes to this plugin are documented here, organized
by release and change type (`Added` / `Fixed` / `Changed` / `Breaking`).

**Policy:** purely internal changes with no effect on the plugin's behavior or
requirements (dependency bumps, CI/build tooling, test environment, scaffold
syncs) are not listed individually here, unless they change something a user
would notice or need to act on — e.g. a new minimum supported Grafana version.

## 1.0.0 - Unreleased

### Added

- Initial release: live streaming of OpenRVDAS CachedDataServer fields into
  Grafana through Grafana Live, at up to 10 Hz. Panels show each field's
  latest value as soon as they load, then update continuously.
- Two connection modes: **OpenRVDAS API**, an authenticated, read-only
  connection through `/api/v1/ws/data-server` with an API key sent in the
  `X-API-Key` header and stored encrypted, and **Direct (legacy)**, an
  unauthenticated connection to the CachedDataServer
  ([#2](https://github.com/OceanDataTools/grafana-openrvdas-datasource/pull/2)).
- Query editor with a field picker that lists the server's fields, `*`
  wildcards and dashboard variables, a per-query minimum interval, and a
  stream toggle for latest-values-only queries.
- Units and descriptions from OpenRVDAS metadata, mapped to Grafana units
  where possible.
- A warning when a field's latest value is older than the panel's time
  range, with the value's age, since that usually means its logger isn't
  running
  ([#10](https://github.com/OceanDataTools/grafana-openrvdas-datasource/pull/10)).
- Supports Grafana 12.0.10+, 12.1.7+, 12.2.5+ and 13.x.
