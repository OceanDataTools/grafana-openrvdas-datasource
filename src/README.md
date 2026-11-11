# OpenRVDAS

Real-time streaming of [OpenRVDAS](https://github.com/OceanDataTools/openrvdas) data into Grafana dashboards.

The data source connects to an OpenRVDAS **CachedDataServer** and streams the fields you choose through Grafana Live at up to 10 Hz. Panels show the current value as soon as they load and then update continuously. It pairs well with the OceanDataTools compass, attitude and windrose panels.

This data source is for **live** data only. For historical data, use the database OpenRVDAS writes to (e.g. InfluxDB) with its own Grafana data source.

## Requirements

- Grafana 12.0.10 or newer (12.0.10+, 12.1.7+, 12.2.5+, 13.x)
- An OpenRVDAS CachedDataServer reachable **from the Grafana server** (default port 8766). Browsers don't need direct access to it.

## Configuration

| Setting | Description |
|---|---|
| **CDS URL** | WebSocket URL of the CachedDataServer, e.g. `ws://openrvdas.local:8766` (`wss://` for TLS). |
| **Update interval (ms)** | How often the CachedDataServer sends new data. Default 100 ms (10 Hz). |
| **Skip TLS verify** | Accept self-signed certificates for `wss://` URLs. |

**Save & test** connects to the server and reports how many fields are available.

## Queries

- **Fields**: pick one or more fields, or type a pattern with `*` (e.g. `S330*`, `Mwx*Wind*`). Dashboard variables can be used.
- **Min interval (ms)**: optionally slow a query down to one value (the most recent) per interval.
- **Stream**: on by default. Turn it off to return only the latest values.

Each field becomes its own series, named after the field, with units and descriptions taken from OpenRVDAS metadata when available.

## Tips for live dashboards

- Leave the dashboard's auto-refresh **off**. Streaming panels update themselves, and auto-refresh only re-runs the initial query.
- For panels that animate (compass, attitude, …), set the animation duration to the update interval or less (e.g. 100 ms or 0). Otherwise the needle lags behind the data.
- To combine fast data (attitude at 10 Hz) with slow data (weather at 1 Hz) efficiently, use **Min interval** on the slow queries, or configure a second data source with a longer update interval.
