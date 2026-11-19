# Contributing

Bug reports, feature requests and pull requests are welcome. Please open an [issue](https://github.com/OceanDataTools/grafana-openrvdas-datasource/issues) first for anything beyond a small fix.

See [docs/DESIGN.md](docs/DESIGN.md) for the architecture and the reasons behind it. Keep it current when you change behavior.

## Installing a development build

Until the plugin is in the Grafana plugin catalog, install a development build by hand. See [docs/INSTALL-DEV.md](docs/INSTALL-DEV.md) for building, installing into an existing Grafana (Linux, Docker, macOS, Windows), allowing the unsigned plugin, configuring a reverse proxy for live streaming, and troubleshooting.

## Development environment

Prerequisites: Node 24.5+ (`.nvmrc`), Go (see `go.mod`), [mage](https://magefile.org), Docker.

```bash
npm install
npm run dev                      # frontend, watch mode
mage -v build:linuxARM64         # backend for the dev container (build:linux on x86_64); rerun after Go changes
npm run server                   # Grafana at http://localhost:3000 with the "OpenRVDAS live" dashboard
```

`npm run server` also starts `cds-sim`, a simulated CachedDataServer (`cmd/cds-sim`) with 10 Hz attitude and 1 Hz navigation and met fields. The provisioned data source uses it by default, through the OpenRVDAS API path with a dummy key, so a fresh checkout works with no OpenRVDAS install. To use a real OpenRVDAS instead, put the connection in a git-ignored `.env`:

```bash
# OpenRVDAS API (authenticated)
CDS_CONNECTION_MODE=proxy
CDS_URL=wss://<host>/api/v1/ws/data-server
OPENRVDAS_API_KEY=<key>
# or direct (legacy, unauthenticated)
CDS_CONNECTION_MODE=direct
CDS_URL=ws://<host>:8766
```

### Winch demo with OpenRVDAS

To see the plugin against stock OpenRVDAS at 10 Hz, rather than the simulator, run [demo/winch/start.sh](demo/winch/README.md). It streams a synthetic CTD cast through a local OpenRVDAS logger and CachedDataServer, and sets up a winch dashboard in the dev Grafana.

## Tests

```bash
go test -race ./pkg/...                                            # Go unit, fake-CDS protocol and simulator tests
CDS_URL=ws://<cds-host>:8766 go test -tags integration -v ./pkg/cds/   # against a real CDS (add OPENRVDAS_API_KEY for the proxy)
npm run typecheck && npm run lint && npm run test:ci               # frontend
npm run e2e                                                        # Playwright; needs the server running (simulator or a live CDS)
GRAFANA_VERSION=12.0.10 npm run server                             # test the oldest supported Grafana
```

CI runs all of these, including e2e against the simulator on a range of Grafana versions, for every pull request.

## Workflow

- Branch from `dev` for each issue (e.g. `issue_123`) and open the pull request against `dev`. Don't commit to `dev` or `main` directly.
- Releases are merges of `dev` into `main`, tagged `vX.Y.Z`. The release workflow builds the plugin zip and a draft GitHub release.
- Add user-facing changes to [CHANGELOG.md](CHANGELOG.md).

## Plugin README

`src/README.md` is the plugin's page in the Grafana catalog and ships in the plugin zip; the root `README.md` is a copy of it. Keep them identical, use only absolute links, and keep installation, build and development instructions here rather than in the README: Grafana's review rejects plugins whose README contains them.

`mage -v` builds the backend for all platforms for release.
