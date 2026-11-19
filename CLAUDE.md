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
- `npm run server`: Grafana in Docker with the provisioned data source and "OpenRVDAS live" dashboard, plus `cds-sim`, the bundled CDS simulator the data source uses by default (proxy mode, dummy key). To use a real OpenRVDAS, set `CDS_URL`, `CDS_CONNECTION_MODE` and `OPENRVDAS_API_KEY` in a git-ignored `.env`. `GRAFANA_VERSION=12.0.10 npm run server` tests the oldest supported Grafana.
- `npm run e2e`: Playwright. Needs built `dist/` and the server running (simulator or a live CDS).
- CI (`.github/workflows/ci.yml`) runs on PRs and pushes to `dev` and `main`; `release.yml` runs on `vX.Y.Z` tags.

## Architecture

Backend (`pkg/`):
- `pkg/cds/client.go`: CDS WebSocket protocol: `fields`, `describe`, `subscribe`, `ready`, one-shot `Latest`. Sends the API key as `X-API-Key` and returns `HandshakeError` (HTTP status) when the handshake is refused.
- `pkg/models/settings.go`: settings, `connectionMode` (`proxy` = OpenRVDAS API with API key, `direct` = legacy unauthenticated CDS; unset means direct), and `Validate`.
- `pkg/cds/manager.go`: one multiplexed CDS connection per data source instance. It re-subscribes when the field set changes (overlap for streaming fields, `seconds:-1` for new ones), de-duplicates by per-field last timestamp, reconnects with backoff, and fans batches out to per-field subscriptions.
- `pkg/plugin/datasource.go`: `QueryData` returns one frame per field with its latest value and `meta.channel` set. `RunStream` forwards one field per Live channel. Also `CheckHealth`, and the `CallResource` `GET fields` route used by the query editor.
- `pkg/plugin/channel.go`: Live channel path `f/<minIntervalMs>/<escaped field>`. Live paths only allow `[A-z0-9_\-/=.]`, so other bytes are `=XX` escaped.
- `pkg/plugin/frames.go`: type inference, frame building, unit mapping, glob expansion.
- `pkg/cdssim`, `cmd/cds-sim`: CDS simulator for the docker compose test environment and CI e2e. Not part of the plugin. The e2e tests and provisioned dashboard depend on its field names and units.

Frontend (`src/`) is thin: `DataSourceWithBackend` auto-subscribes to `meta.channel` frames, so there is no custom streaming code. The query editor uses `MultiCombobox` with custom values for wildcard patterns.

## Docs

- `docs/DESIGN.md`: architecture and the reasons behind it. Keep it current.
- `demo/winch/`: a 10 Hz winch demo through stock OpenRVDAS (logger + CachedDataServer from a local checkout), with its own Grafana data source and dashboard created by `start.sh`. Not part of `provisioning/`.
- `docs/INSTALL-DEV.md`: installing an unsigned development build into an existing Grafana. Temporary: delete it, and its link in `CONTRIBUTING.md`, once the plugin is in the Grafana catalog.
- `src/README.md` is the catalog page and ships in `dist/`. The root `README.md` is an identical copy. Use only absolute links, and keep installation, build and development instructions out of both: Grafana's review rejects READMEs that contain them. Those go in `CONTRIBUTING.md`.
- `CHANGELOG.md`: user-facing changes only, in the sibling panels' format (`## X.Y.Z - YYYY-MM-DD`, Added/Fixed/Changed/Breaking, PR links).

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
