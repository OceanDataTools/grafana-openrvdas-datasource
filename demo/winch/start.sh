#!/usr/bin/env bash
# Winch demo: stream a synthetic CTD cast at 10 Hz through a local OpenRVDAS
# into a CachedDataServer, and (optionally) set up a Grafana data source and
# dashboard for it. See README.md. Ctrl-C stops everything.
#
# Environment:
#   OPENRVDAS_DIR  OpenRVDAS checkout with a venv (default: ../../openrvdas
#                  relative to the repo root, i.e. a sibling of grafana-plugins/)
#   CDS_PORT       CachedDataServer port (default 8766)
#   UDP_PORT       local UDP port from the logger to the CDS (default 6230)
#   ACCEL          LogfileReader time_acceleration_factor; raise it slightly
#                  if the measured rate is below 10 Hz (default 1.08)
#   GRAFANA_URL    Grafana to configure (default http://localhost:3000);
#                  set to "" to skip
#   GRAFANA_AUTH   user:password for Grafana's API, if anonymous admin is off
#   CDS_URL_FROM_GRAFANA  CDS URL as seen by Grafana
#                  (default ws://host.docker.internal:$CDS_PORT, for the
#                  docker compose dev Grafana)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
OPENRVDAS_DIR="${OPENRVDAS_DIR:-$(cd "$REPO/../.." && pwd)/openrvdas}"
CDS_PORT="${CDS_PORT:-8766}"
UDP_PORT="${UDP_PORT:-6230}"
ACCEL="${ACCEL:-1.08}"
GRAFANA_URL="${GRAFANA_URL-http://localhost:3000}"
CDS_URL_FROM_GRAFANA="${CDS_URL_FROM_GRAFANA:-ws://host.docker.internal:$CDS_PORT}"
PY="$OPENRVDAS_DIR/venv/bin/python"
DATA="$HERE/data"
LOG="$DATA/ctd_winch.log"

if [[ ! -x "$PY" ]]; then
  echo "No OpenRVDAS venv at $OPENRVDAS_DIR/venv. Set OPENRVDAS_DIR." >&2
  exit 1
fi
if lsof -nP -iTCP:"$CDS_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Port $CDS_PORT is already in use. Stop that server or set CDS_PORT." >&2
  exit 1
fi

mkdir -p "$DATA"
if [[ ! -s "$LOG" ]]; then
  echo "Generating $LOG"
  python3 "$HERE/generate_winch_log.py" > "$LOG"
fi

pids=()
cleanup() {
  trap - INT TERM EXIT
  echo "Stopping winch demo"
  # Children first: the logger loop's current listen.py, then the loop itself.
  for pid in "${pids[@]}"; do pkill -P "$pid" 2>/dev/null || true; kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

cd "$OPENRVDAS_DIR"

# 1. CachedDataServer, the same server OpenRVDAS runs in production. It
# serves websocket clients on CDS_PORT and takes records on UDP_PORT.
"$PY" server/cached_data_server.py --port "$CDS_PORT" --udp "$UDP_PORT" &
pids+=($!)

# 2. The winch logger (winch_logger.yaml): replays the cast at 10 Hz, stamps
# and parses each record with OpenRVDAS's Winch device type, and sends it to
# the CDS. Restarted at the end of each cast so the demo loops.
sed -e "s#__LOG_BASE__#$DATA/ctd_winch#" -e "s#__DEVICES__#$HERE/winch_devices.yaml#" \
    -e "s#__UDP_PORT__#$UDP_PORT#" -e "s#__ACCEL__#$ACCEL#" \
    "$HERE/winch_logger.yaml" > "$DATA/winch_logger.yaml"
(
  while true; do
    "$PY" logger/listener/listen.py --config_file "$DATA/winch_logger.yaml"
  done
) &
pids+=($!)

# 3. Grafana data source and dashboard, through Grafana's HTTP API.
if [[ -n "$GRAFANA_URL" ]]; then
  # A function rather than an array of curl options: bash 3.2 (macOS) treats
  # an empty array as unset under `set -u`.
  gcurl() {
    if [[ -n "${GRAFANA_AUTH:-}" ]]; then curl -sf -u "$GRAFANA_AUTH" "$@"; else curl -sf "$@"; fi
  }
  if gcurl "$GRAFANA_URL/api/health" >/dev/null; then
    ds=$(sed "s#__CDS_URL__#$CDS_URL_FROM_GRAFANA#" "$HERE/grafana/datasource.json")
    if gcurl "$GRAFANA_URL/api/datasources/uid/openrvdas-winch-demo" >/dev/null; then
      gcurl -X PUT -H 'Content-Type: application/json' \
        "$GRAFANA_URL/api/datasources/uid/openrvdas-winch-demo" -d "$ds" >/dev/null
    else
      gcurl -X POST -H 'Content-Type: application/json' \
        "$GRAFANA_URL/api/datasources" -d "$ds" >/dev/null
    fi
    gcurl -X POST -H 'Content-Type: application/json' \
      "$GRAFANA_URL/api/dashboards/db" \
      -d "{\"overwrite\": true, \"dashboard\": $(cat "$HERE/grafana/dashboard.json")}" >/dev/null
    echo "Grafana: $GRAFANA_URL/d/openrvdas-winch-demo"
  else
    echo "Grafana not reachable at $GRAFANA_URL; skipping its setup." >&2
  fi
fi

echo "Winch demo running: CachedDataServer on ws://localhost:$CDS_PORT. Ctrl-C to stop."
wait
