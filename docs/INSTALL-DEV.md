# Installing a development build

> **Temporary.** These instructions are only needed until the plugin is
> accepted into the Grafana plugin catalog. After that, install it from
> *Administration → Plugins and data → Plugins* like any other plugin, and
> delete this file.

A development build is **unsigned**, so Grafana has to be told to load it.
Unlike a panel plugin, it also contains a compiled **backend binary** that
must match the Grafana server's OS and CPU and must be executable.

**Requirements:** Grafana 12.0.10 or newer (12.0.10+, 12.1.7+, 12.2.5+ or
13.x), and access to an OpenRVDAS server from the Grafana host. See the
[README](../README.md#configuration) for the connection options.

## Install from a release (no build needed)

Each [GitHub release](https://github.com/OceanDataTools/grafana-openrvdas-datasource/releases)
has a ready-made zip with backends for every supported platform, e.g.
`oceandatatools-openrvdas-datasource-1.0.0.zip`, and its `.sha1` checksum.
Releases are unsigned until the plugin is accepted into the catalog, so you
still have to allow the unsigned plugin.

**Linux package, Homebrew or Windows:** install with the Grafana CLI, then
allow the plugin and restart Grafana as described in
[Linux package](#linux-package) or [Homebrew and Windows](#homebrew-and-windows):

```bash
sudo grafana cli \
  --pluginUrl https://github.com/OceanDataTools/grafana-openrvdas-datasource/releases/download/v1.0.0/oceandatatools-openrvdas-datasource-1.0.0.zip \
  plugins install oceandatatools-openrvdas-datasource
```

**Docker:** let Grafana install it at startup:

```bash
docker run -d --name grafana -p 3000:3000 \
  -e GF_PLUGINS_PREINSTALL=oceandatatools-openrvdas-datasource@1.0.0@https://github.com/OceanDataTools/grafana-openrvdas-datasource/releases/download/v1.0.0/oceandatatools-openrvdas-datasource-1.0.0.zip \
  -e GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=oceandatatools-openrvdas-datasource \
  grafana/grafana:13.2.4
```

For a newer release, change the version in the URL. Then continue with
[3. Verify](#3-verify). To build from source instead, follow the steps below.

## 1. Build the plugin

You can build on any machine. A full build contains backends for every
supported platform: Linux amd64, arm64 and arm, macOS amd64 and arm64, and
Windows amd64.

Prerequisites:

- **Node 24.5 or newer** (`.nvmrc` and `package.json` `engines`). Distribution
  packages are often older. `nvm install` (which reads `.nvmrc`) or
  [NodeSource](https://github.com/nodesource/distributions) work.
- **Go, at the version in `go.mod`** (currently 1.26.5). On Ubuntu 24.04 the
  `golang` package is 1.22, which is too old, and Ubuntu's Go doesn't download
  newer toolchains by default. Install Go from [go.dev](https://go.dev/dl/), or
  run `export GOTOOLCHAIN=auto` first so Go fetches the toolchain `go.mod`
  asks for.
- **[mage](https://magefile.org)**: `go install github.com/magefile/mage@latest`.
  This puts `mage` in `$(go env GOPATH)/bin` (usually `~/go/bin`), which may
  not be on your `PATH`: `export PATH="$PATH:$(go env GOPATH)/bin"`.

```bash
git clone https://github.com/OceanDataTools/grafana-openrvdas-datasource.git
cd grafana-openrvdas-datasource
npm ci
npm run build   # frontend  -> dist/
mage -v         # backends for all platforms -> dist/gpx_openrvdas_<os>_<arch>
```

Package `dist/` under the plugin's ID. `tar` keeps the execute permission on
the backend binaries:

```bash
cp -R dist oceandatatools-openrvdas-datasource
tar -czf openrvdas-datasource-dev.tar.gz oceandatatools-openrvdas-datasource
rm -rf oceandatatools-openrvdas-datasource
```

## 2. Install into Grafana

Unpack the archive into Grafana's plugins directory, so that you end up with
`<plugins dir>/oceandatatools-openrvdas-datasource/plugin.json`.

| Grafana install | Plugins directory | Config file |
|---|---|---|
| Linux package (deb/rpm) | `/var/lib/grafana/plugins` | `/etc/grafana/grafana.ini` |
| Docker image | `/var/lib/grafana/plugins` (mount a volume) | environment variables |
| Homebrew (macOS, Apple Silicon) | `/opt/homebrew/var/lib/grafana/plugins` | `/opt/homebrew/etc/grafana/grafana.ini` |
| Homebrew (macOS, Intel) | `/usr/local/var/lib/grafana/plugins` | `/usr/local/etc/grafana/grafana.ini` |
| Windows | `<Grafana install dir>\data\plugins` | `<Grafana install dir>\conf\custom.ini` |

If `[paths] plugins` is set in your config file, that location wins.

### Linux package

```bash
sudo tar -C /var/lib/grafana/plugins -xzf openrvdas-datasource-dev.tar.gz
sudo chown -R grafana:grafana /var/lib/grafana/plugins/oceandatatools-openrvdas-datasource
sudo chmod +x /var/lib/grafana/plugins/oceandatatools-openrvdas-datasource/gpx_openrvdas_*
```

Allow the unsigned plugin in `/etc/grafana/grafana.ini`. If the setting
already lists other plugins, add this one with a comma:

```ini
[plugins]
allow_loading_unsigned_plugins = oceandatatools-openrvdas-datasource
```

Then restart Grafana:

```bash
sudo systemctl restart grafana-server
```

### Docker

Unpack the archive into a directory on the host, then mount it as the
plugins directory and allow the plugin through the environment:

```bash
mkdir -p ./grafana-plugins
tar -C ./grafana-plugins -xzf openrvdas-datasource-dev.tar.gz

docker run -d --name grafana -p 3000:3000 \
  -v "$PWD/grafana-plugins:/var/lib/grafana/plugins" \
  -e GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=oceandatatools-openrvdas-datasource \
  grafana/grafana:13.2.4
```

In docker compose, add the same volume and environment entry to the Grafana
service. Mounting over `/var/lib/grafana/plugins` hides any plugins already
installed in the image's volume. If you have others, put them in the same
host directory, or mount just this plugin at
`/var/lib/grafana/plugins/oceandatatools-openrvdas-datasource`.

### Homebrew and Windows

Unpack into the directory from the table, set
`allow_loading_unsigned_plugins` as for the Linux package, and restart
Grafana (`brew services restart grafana`, or restart the Grafana service on
Windows).

## 3. Verify

1. **Check that the plugin is installed.** Under *Administration → Plugins
   and data → Plugins*, search for **OpenRVDAS**. It should be listed and
   marked *unsigned*.
2. **Check the connection.** Under *Connections → Data sources → Add data
   source*, choose **OpenRVDAS**, configure the connection (see the
   [README](../README.md#configuration)), and click **Save & test**. It
   should report `Connected to the … : N fields available`.
3. **Check that data streams.** Add a panel that uses a frequently updating
   field. Its value should keep changing without refreshing the dashboard.

## Reverse proxy in front of Grafana

The plugin streams through **Grafana Live**, which uses a WebSocket at
`/api/live/ws`. If Grafana sits behind nginx (or another proxy), the proxy
must forward WebSocket upgrades for `/api/live/` **and** pass the original
`Host` header including the port. Grafana Live compares it with the
browser's `Origin`.

```nginx
map $http_upgrade $connection_upgrade {
  default upgrade;
  ''      close;
}

server {
  # ... listen / server_name / TLS ...

  location / {
    proxy_pass http://grafana:3000;
    proxy_set_header Host $http_host;
  }

  location /api/live/ {
    proxy_pass http://grafana:3000;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $http_host;
  }
}
```

Both points were tested against Grafana 13.2.4 behind nginx:

- **Without the `/api/live/` block**, the WebSocket handshake fails with
  **400**.
- **With `Host $host` instead of `$http_host`**, it fails with **403**,
  because `$host` drops the port.

In both cases panels show the first value and then never update. The data
source's health check still passes, so this is easy to miss.

## Upgrading

Build and package as in step 1, then delete the old
`oceandatatools-openrvdas-datasource` directory, unpack the new one, and
restart Grafana. Your data sources and dashboards are stored in Grafana's
database and are not affected.

## Troubleshooting

Look in the Grafana server log (`/var/log/grafana/grafana.log`, or
`docker logs grafana`) for lines mentioning
`oceandatatools-openrvdas-datasource`.

| Symptom | Log message | Fix |
|---|---|---|
| OpenRVDAS can't be added as a data source; Grafana reports "Plugin signature is missing" | `Skipping loading plugin due to problem with signature … status=unsigned` | Set `allow_loading_unsigned_plugins` / `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS` exactly to `oceandatatools-openrvdas-datasource`, then restart |
| *Save & test* or queries fail with "Plugin not registered" or "Plugin failed to start" | `Could not start plugin backend … fork/exec …/gpx_openrvdas_<os>_<arch>: permission denied` | `chmod +x <plugins dir>/oceandatatools-openrvdas-datasource/gpx_openrvdas_*`, then restart |
| Same | `Could not start plugin backend … fork/exec …/gpx_openrvdas_<os>_<arch>: no such file or directory` | The build has no backend for this OS/CPU. Rebuild with `mage -v` (all platforms) and reinstall |
| Panels show one value, then stop updating | requests to `/api/live/ws` returning 400 or 403 | Reverse proxy configuration; see above |
| *Save & test* reports an authentication or connection problem | | See the connection settings in the [README](../README.md#configuration) |
