# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project knowledge

This repository contains a **Grafana plugin**. You must Read @./.config/AGENTS/instructions.md before doing changes.

## What this is

A Grafana backend data source plugin (`oceandatatools-openrvdas-datasource`) that streams live fields from an OpenRVDAS CachedDataServer (CDS) into Grafana via Grafana Live, targeting up to 10 Hz. It is live-only: no historical queries. **Read `docs/DESIGN.md` before changing behavior** and keep it up to date when the design changes.

## Commands

- `npm run dev` / `npm run build`: frontend (webpack) into `dist/`
- `mage -v build:linuxARM64` (Apple Silicon dev container) or `mage -v build:linux`: backend into `dist/`. Rerun after every Go change, then restart the container. `mage -v` builds all platforms.
- `npm run typecheck`, `npm run lint`, `npm run test:ci`: frontend checks
- `go vet ./pkg/... && go test -race ./pkg/...`: backend unit tests, including a fake CDS for protocol tests
- `CDS_URL=… [OPENRVDAS_API_KEY=…] [OPENRVDAS_PROXY_URL=…] go test -tags integration -v ./pkg/cds/`: against a real CDS or proxy
- `npm run server`: Grafana in Docker with the provisioned data source and "OpenRVDAS live" dashboard. The connection comes from a git-ignored `.env`: `CDS_URL`, plus `CDS_CONNECTION_MODE=proxy` and `OPENRVDAS_API_KEY` for the OpenRVDAS API. `GRAFANA_VERSION=12.0.10 npm run server` tests the oldest supported Grafana.
- `npm run e2e`: Playwright. Needs built `dist/`, the server running, and a CDS with live data.

## Architecture

Backend (`pkg/`):
- `pkg/cds/client.go`: CDS WebSocket protocol: `fields`, `describe`, `subscribe`, `ready`, one-shot `Latest`. Sends the API key as `X-API-Key` and returns `HandshakeError` (HTTP status) when the handshake is refused.
- `pkg/models/settings.go`: settings, `connectionMode` (`proxy` = OpenRVDAS API with API key, `direct` = legacy unauthenticated CDS; unset means direct), and `Validate`.
- `pkg/cds/manager.go`: one multiplexed CDS connection per data source instance. It re-subscribes when the field set changes (overlap for streaming fields, `seconds:-1` for new ones), de-duplicates by per-field last timestamp, reconnects with backoff, and fans batches out to per-field subscriptions.
- `pkg/plugin/datasource.go`: `QueryData` returns one frame per field with its latest value and `meta.channel` set. `RunStream` forwards one field per Live channel. Also `CheckHealth`, and the `CallResource` `GET fields` route used by the query editor.
- `pkg/plugin/channel.go`: Live channel path `f/<minIntervalMs>/<escaped field>`. Live paths only allow `[A-z0-9_\-/=.]`, so other bytes are `=XX` escaped.
- `pkg/plugin/frames.go`: type inference, frame building, unit mapping, glob expansion.

Frontend (`src/`) is thin: `DataSourceWithBackend` auto-subscribes to `meta.channel` frames, so there is no custom streaming code. The query editor uses `MultiCombobox` with custom values for wildcard patterns.

## Invariants

- **Value column name.** It must equal the CDS field name: OceanDataTools panels match `field.name`.
- **Streamed frame schema.** Streamed frames are `data.IncludeDataOnly` and must match the schema of the `QueryData` frame: `time` + value, same type.
- **Wildcard semantics.** Wildcards are anchored globs expanded by the plugin, never sent to CDS, whose own `*` matching is an unanchored regex.
- **Frontend API range.** Only use frontend APIs available across the supported Grafana range in `src/plugin.json` (matches the OceanDataTools panels). Run e2e against 12.0.10 as well as the default version.
- **No CDS server changes.** The plugin must work with stock OpenRVDAS.
- **API key handling.** The key lives only in `secureJsonData`, is sent only in proxy mode, and only as the `X-API-Key` header, never as `?api_key=`, which would end up in access logs.
- **Both connection modes.** Everything above `cds.Dial` must stay mode-agnostic; the proxy relays the CDS protocol unchanged.

## The `.config/` directory

Managed by `@grafana/create-plugin`; do not edit. Extend via the root-level config files.

## Git workflow

- Never commit directly to `dev` (or `main`). All code changes go on an issue branch created off `dev` (e.g. `issue_NNN`).
- Merge an issue branch into `dev` only via a pull request.
