# OpenRVDAS data source: design

This document records how the plugin is built and why. It is aimed at
maintainers; see the [README](../README.md) for usage.

## Goal and scope

Stream live OpenRVDAS data into Grafana dashboards at up to **10 Hz**, mainly
for single-value "instrument" panels such as the OceanDataTools compass,
attitude and windrose panels, plus stat/gauge panels and short scrolling time
series.

Deliberately **out of scope**:

- **Historical queries.** The data source serves the live value and what
  accumulates while a dashboard is open. Long-term history belongs in the
  database OpenRVDAS already writes to (e.g. InfluxDB) and its own Grafana
  data source.
- **Alerting.** Possible later (see [Future work](#future-work)), but not a
  design driver.
- **Writing to OpenRVDAS.** The plugin is read-only.

## Data source: the CachedDataServer

The plugin talks to the OpenRVDAS **CachedDataServer** (CDS,
`openrvdas/server/cached_data_server.py`) over its WebSocket API. No CDS
changes are required; any CDS version with the WebSocket API works.

| Request | Used for |
|---|---|
| `{"type":"fields"}` | Field picker, wildcard expansion, health check |
| `{"type":"describe","fields":[...]}` | Units and descriptions |
| `{"type":"subscribe","interval":s,"fields":{f:{"seconds":n}}}` | Streaming and one-shot latest values |
| `{"type":"ready"}` | Pull the next batch |

Protocol details that shaped the design:

- **The client pulls each batch.** After answering a `ready`, the CDS sleeps for whatever
  is left of the subscription `interval` (measured from when it started
  waiting for that `ready`) before reading the next message. A client that
  replies with `ready` immediately gets one batch per `interval`.
- **The interval is per connection.** Every field on one connection is
  delivered at the same cadence.
- **`seconds` semantics.** `-1` means the most recent value, then new values.
  `0` means new values only. `N` means N seconds of back data, then new values.
- **Re-subscribing resets server state.** A new `subscribe` replaces the
  field set and resets the server's per-field "last sent" timestamps.
- **The CDS HTTP API is not used.** `GET /fields` and `/latest/...` exist only
  with websockets ≥ 12 on the server, and the CDS documentation warns against
  polling them.
- **Wildcards are expanded by the plugin.** CDS turns `*` into the regex
  `.+` and matches unanchored, so `Mwx*` also matches `AvgMwxAirTemp`. The
  plugin instead expands `*` itself as an anchored glob against the field list,
  and only ever subscribes to concrete names (see below).

## Architecture

```
                    one WebSocket per data source instance
 CachedDataServer  ◄──────────────────────────────────────►  Go backend (pkg/)
   subscribe(interval=0.1s, {all active fields})               cds.Manager
   ready ↔ data batches                                           │ fan-out per field
                                                                  ▼
                                                     RunStream("f/0/S330Roll") …
                                                                  │ Grafana Live
                                                                  ▼
                                    browser: DataSourceWithBackend → panels
```

### Why a Go backend

A frontend-only plugin would open a WebSocket from every browser tab to the
CDS. That fails in common shipboard setups:

- **Reachability.** Shore-side browsers often can't reach the ship's CDS.
- **Mixed content.** HTTPS Grafana pages can't open plain `ws://` connections.
- **Load.** Every tab would add load to the CDS event loop.

With the backend, only the Grafana server talks to the CDS, and Grafana Live
fans each stream out to any number of viewers.

### Grafana Live channels: one per field

Each CDS field is its own Live channel:

```
ds/<datasource uid>/f/<minIntervalMs>/<escaped field name>
```

- **Sharing between panels.** Two panels or dashboards that use the same
  field share one `RunStream`, even if their queries list different fields.
  Per-query channels would only share identical queries.
- **Simple frames.** A field's frame is always `time` plus one value column.
  Fields arrive at different timestamps, so a wide multi-field frame would
  be mostly nulls.
- **Escaping.** Live paths may only contain `[A-z0-9_\-/=.]`, but CDS names can
  contain other characters (`status:logger_status`). Unsafe bytes, and `=`
  itself, become `=XX` hex escapes, so ordinary names stay readable:
  `status:logger_status` → `status=3Alogger_status` (`pkg/plugin/channel.go`).
- **`minIntervalMs` in the path.** A decimated stream is a separate channel
  from the full-rate one, so a slow panel never slows a fast one.

### Query flow

1. **The frontend sends the query.** `DataSourceWithBackend` passes the query
   (`fields`, `minIntervalMs`, `stream`) to the backend after expanding
   dashboard variables.
2. **`QueryData` builds one frame per field** (`pkg/plugin/datasource.go`):
   - It expands wildcards using the cached `fields` list (10 s TTL).
   - It gets metadata from `describe` (5 min TTL) and maps it to a Grafana unit
     and description.
   - It takes the latest value from the connection manager if the field is
     already streaming. Otherwise it uses a one-shot `subscribe` with
     `seconds:-1` over a short-lived connection.
   - It returns one frame per field, with `meta.channel` set when streaming.
3. **The runtime subscribes.** `DataSourceWithBackend` sees `meta.channel`,
   subscribes to each channel, and uses the returned frame as the stream's
   initial data. The panel therefore renders the current value immediately,
   with units, without waiting for the next update. No custom streaming code
   is needed in the frontend.
4. **`RunStream` forwards the field.** Grafana starts one `RunStream` per
   channel. It subscribes the field with the manager and forwards each batch as
   a data-only frame. With `minIntervalMs > 0`, a ticker forwards only the
   most recent value per interval.
5. **Teardown.** When the last viewer leaves a channel, Grafana cancels
   `RunStream`, which unsubscribes from the manager.

### Connection manager (`pkg/cds/manager.go`)

One `Manager` per data source instance owns the streaming connection:

- **Lazy lifecycle.** It connects when the first field is subscribed,
  re-sends `subscribe` with the full field set whenever that set changes,
  and closes the connection when the last field goes away.
- **Starting points.** Newly added fields request `seconds:-1`, so they start
  from the current value. Fields that were already streaming request a
  10-second overlap, so nothing is lost across a re-subscribe or reconnect.
- **De-duplication.** The manager tracks the last delivered timestamp per
  field and drops anything at or before it. That makes the overlap safe.
- **Reconnects.** After a failure it reconnects with exponential backoff
  (1 s to 30 s). A connection that receives nothing for max(10 s,
  20 × interval) is treated as dead.
- **Slow consumers.** Each subscriber has a 64-batch buffer. If a consumer
  falls behind, new batches are dropped for that consumer only, with a
  warning, and the shared connection keeps going.
- **Latest values.** It keeps the latest point of every streaming field, so
  `QueryData` can answer for that field without a CDS round trip.

A new data source instance (Grafana creates one whenever settings change)
gets a new manager. The old one is closed in `Dispose`.

### Frames and types

- **Naming.** The frame and its value column are both named after the CDS
  field, because the OceanDataTools panels look columns up with
  `field.name === <configured field>`. Never rename the value column to
  something generic like `value`.
- **Column type.** The type is chosen from the field's latest value and then
  fixed for the stream, because streamed frames are data-only and must match
  the initial frame's schema:
  - number → nullable float64
  - bool → nullable bool
  - string, object or list → nullable string (objects and lists are
    JSON-encoded)
  - Values that don't fit the column type are converted where sensible
    (e.g. `"12.5"` → 12.5) and become null otherwise.
- **Units.** Common OpenRVDAS unit strings map to Grafana unit IDs (`degrees`
  → `degree`, `knots` → `velocityknot`, `C` → `celsius`, …). Unknown units
  are shown as a suffix (`pkg/plugin/frames.go`).

### Update rate

End-to-end cadence is set in three places:

1. **The CDS interval.** This is the data source's *Update interval*
   (default 100 ms, minimum 20 ms) and is shared by every field on the
   connection.
2. **Per-query decimation.** `minIntervalMs` can only slow a stream down,
   never speed it up.
3. **The browser.** The runtime's streaming buffer keeps at most
   `maxDataPoints` rows (and the dashboard time span when the range ends at
   `now`), so memory stays bounded.

Panels with CSS transitions should use animation durations no longer than
the update interval. For example, the compass and attitude panels default to
600 ms, which makes a 10 Hz needle lag; set it to about 80–100 ms or 0.

## Configuration

| jsonData key | Default | Meaning |
|---|---|---|
| `url` | (required) | CDS WebSocket URL. `ws://`, `wss://`, `http(s)://` or bare `host:port` are accepted. |
| `pollIntervalMs` | 100 | CDS subscription interval |
| `tlsSkipVerify` | false | Accept self-signed certificates for `wss://` |

No secrets: the CDS has no authentication.

Query model:

```jsonc
{
  "fields": ["S330HeadingTrue", "Mwx*"], // names or '*' globs; dashboard variables allowed
  "minIntervalMs": 250,                   // optional decimation
  "stream": true                          // false = latest values only
}
```

## Compatibility

- **Grafana.** `>=12.0.10 <12.1 || >=12.1.7 <12.2 || >=12.2.5`, the same range
  as the OceanDataTools panels. The e2e suite has been run against 12.0.10 and
  13.2.4.
- **Frontend APIs.** The frontend only uses APIs that exist across that
  range. Live streaming via `meta.channel` predates Grafana 12, and
  `MultiCombobox` is available in 12.0.
- **CDS.** Any version with the WebSocket API.

## Testing

| Layer | Where | Notes |
|---|---|---|
| Go unit | `pkg/plugin/*_test.go` | Channel escaping, wildcard expansion, type inference, frames, units |
| Go protocol | `pkg/cds/manager_test.go` | In-process fake CDS: subscribe, de-dup across re-subscribe, unsubscribe/close, reconnect |
| Go integration | `pkg/cds/integration_test.go` | `CDS_URL=ws://host:8766 go test -tags integration ./pkg/cds/` |
| E2E | `tests/*.spec.ts` | Config editor, query editor, and live messages reaching the browser on the provisioned dashboard |

## Known limitations and future work

- **10 Hz is not yet proven end to end.** The dev CDS used so far only
  updates at about 1–2 Hz. A direct client of that CDS received batches
  every ~120 ms at a 100 ms interval over the internet, but a 10 Hz load test with real
  attitude data and a full dashboard is still to be done. Browser rendering
  is the expected bottleneck.
- **Wildcards are expanded at query time.** A field that starts matching
  later appears on the next query run (panel refresh or dashboard reload).
- **One interval per data source.** To mix fast attitude data with slow
  weather data efficiently, configure two data source instances. Per-query
  intervals on separate connections are possible later if needed.
- **Duplicate point when joining.** A panel that joins a channel may see its
  initial value once more as the first streamed point. Instrument panels are
  unaffected; a time series shows one duplicate point.
- **Alerting.** `QueryData` already works without a browser (`stream: false`).
  Enabling `"alerting": true` in `plugin.json` and testing it is
  straightforward follow-up work.
