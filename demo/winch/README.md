# Winch demo: OpenRVDAS at 10 Hz

A Grafana dashboard of CTD winch data (wire out, wire speed, tension) updating
at 10 Hz, fed by a real OpenRVDAS pipeline on your machine: a logger parsing
winch records with OpenRVDAS's own `Winch` device type into a CachedDataServer.
The winch data is a synthetic CTD cast replayed from a log file.

Unlike the `cds-sim` simulator that `docker compose up` provides for plugin
reviewers and CI, this exercises stock OpenRVDAS end to end.

## Requirements

- An OpenRVDAS checkout with its venv (`venv/bin/python`), by default at
  `../../openrvdas` relative to this repo's root (a sibling of
  `grafana-plugins/`). Set `OPENRVDAS_DIR` otherwise. No OpenRVDAS changes are
  needed.
- The dev Grafana from `npm run server` (or any Grafana with this plugin) on
  `http://localhost:3000`.

## Run

```bash
demo/winch/start.sh
```

Then open <http://localhost:3000/d/openrvdas-winch-demo>. Ctrl-C stops
everything.

`start.sh`:

1. Generates `data/ctd_winch.log` the first time (`generate_winch_log.py`): one
   CTD cast at 10 Hz, about 21 minutes: idle on deck, lift, lower to 500 m at
   60 m/min, soak, haul back, recover. Tension follows the package weight, wire
   out, drag and swell. Records use the NBP / LCI-90i winch format:
   `01RD,<time>,CTD,<tension lbs>,<speed m/min>,<payout m>,<checksum>`.
2. Starts `server/cached_data_server.py` on port 8766 (websocket) and 6230
   (UDP in).
3. Runs the logger in `winch_logger.yaml`, restarting it after each cast so
   the demo loops: replay the log at its own pace, stamp each record with the
   current time, tag it `ctdw`, parse it with the `Winch` device type into
   `CTDWinchPayout`, `CTDWinchSpeed`, `CTDWinchTension` and friends
   (`winch_devices.yaml`), and send it to the CDS.
4. Creates or updates the **OpenRVDAS winch demo** data source (direct mode,
   `ws://host.docker.internal:8766`, 100 ms update interval) and the
   **OpenRVDAS winch (10 Hz demo)** dashboard through Grafana's HTTP API. They
   are not part of `provisioning/`, which stays the reviewer and CI
   environment.

Options, as environment variables: `OPENRVDAS_DIR`, `CDS_PORT`, `UDP_PORT`,
`GRAFANA_URL` (empty to skip Grafana setup), `GRAFANA_AUTH` (`user:password`
if anonymous admin is off), `CDS_URL_FROM_GRAFANA` (if Grafana isn't the docker
compose one), and `ACCEL` (see below). For a different cast, delete
`data/ctd_winch.log` and run `generate_winch_log.py --help`, e.g.
`--max-payout 1000 --speed 45`.

## Measured on a MacBook (Apple Silicon)

| Where                                            | Rate                                                |
| ------------------------------------------------ | --------------------------------------------------- |
| CachedDataServer subscriber at a 100 ms interval | 10 Hz, points 93–107 ms apart, median latency 64 ms |
| Grafana Live messages in the browser, per field  | 9.8 per second                                      |

## Why the logger is set up this way

Two stock-OpenRVDAS behaviors would otherwise hold the demo below a steady
10 Hz:

- **`CachedDataWriter` batches.** When its queue is empty it sleeps 200 ms, so
  10 Hz records reach the CDS in pairs, five times a second. The logger sends
  JSON over UDP to `127.0.0.1` instead, which the CDS caches per record
  (`cached_data_server.py --udp`). Unicast to localhost, so nothing is
  broadcast on your network.
- **`LogfileReader` pacing drifts.** With `use_timestamps` it sleeps each
  record's recorded gap after the previous record was returned, so per-record
  overhead (~8 ms here) adds up to about 9.3 Hz. `time_acceleration_factor`
  (`ACCEL`, default 1.08) compensates; raise it slightly on a slower machine.

Also, if you run `listen.py` with command-line options instead of a config
file, order matters: options such as `--logfile_use_timestamps` and
`--parse_definition_path` only apply to readers and transforms that come
_after_ them on the command line.
