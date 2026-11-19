#!/usr/bin/env python3
"""Generate a synthetic CTD-cast winch log for the OpenRVDAS winch demo.

Writes timestamped records in the NBP / LCI-90i winch format that stock
OpenRVDAS parses with its `Winch` device type (contrib/devices):

    2026-01-01T00:01:00.000000Z 01RD,2026-01-01T00:01:00.000,CTD,00001412,000060.0,000123.4,1834

i.e. `<log timestamp> <LANID>,<winch time>,<name>,<tension lbs>,<speed m/min>,
<payout m>,<checksum>`. OpenRVDAS's simulate_data.py replays the file at its
original rate and loops it, so the cast starts and ends idle on deck.

One cast: idle on deck, lift over the side, lower to --max-payout at
--speed, soak, haul back, recover, idle. Tension is the package's weight
(in air or in water) plus the weight of the wire out, drag that lowers
tension while paying out and raises it while hauling, and swell-induced
dynamic load. Values are deterministic, so every run produces the same file.

Usage: generate_winch_log.py [--max-payout 500] [--speed 60] [--rate 10] > ctd_winch.log
"""
import argparse
import datetime
import math
import sys

LANID = "01RD"
NAME = "CTD"

AIR_WEIGHT = 1800.0   # lbs, CTD rosette in air
WET_WEIGHT = 1250.0   # lbs, in water
WIRE_WEIGHT = 0.35    # lbs per meter of wire out, in water
DRAG = 150.0          # lbs at 1 m/s wire speed
IN_WATER = 4.0        # m of payout at which the package is submerged


def swell(t):
    """Dynamic load from ship heave, lbs."""
    return (110 * math.sin(2 * math.pi * t / 8.5)
            + 35 * math.sin(2 * math.pi * t / 5.3 + 1.0)
            + 8 * math.sin(2 * math.pi * t / 1.7 + 2.0))


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--max-payout", type=float, default=500, help="deepest payout, m (default 500)")
    ap.add_argument("--speed", type=float, default=60, help="cruise wire speed, m/min (default 60)")
    ap.add_argument("--accel", type=float, default=0.06, help="wire acceleration, m/s^2 (default 0.06)")
    ap.add_argument("--rate", type=float, default=10, help="records per second (default 10)")
    ap.add_argument("--deck-time", type=float, default=45, help="idle on deck before and after, s")
    ap.add_argument("--soak", type=float, default=60, help="stop at depth, s")
    ap.add_argument("--start", default="2026-01-01T00:00:00", help="first timestamp (UTC)")
    args = ap.parse_args()

    dt = 1.0 / args.rate
    t0 = datetime.datetime.fromisoformat(args.start).replace(tzinfo=datetime.timezone.utc)
    state = {"i": 0, "payout": 0.0, "lifted": 0.0}
    out = sys.stdout

    def emit(v, tension):
        """Write one record at wire speed v (m/s, + paying out)."""
        i = state["i"]
        t = i * dt
        ts = t0 + datetime.timedelta(seconds=t)
        p = state["payout"]
        rec = (f"{LANID},{ts.strftime('%Y-%m-%dT%H:%M:%S.')}{ts.microsecond // 1000:03d},{NAME},"
               f"{int(round(tension)):08d},{v * 60:08.1f},{p:08.1f}")
        cksum = sum(rec.encode()) % 10000
        out.write(f"{ts.strftime('%Y-%m-%dT%H:%M:%S.%f')}Z {rec},{cksum:04d}\n")
        state["i"] += 1

    def tension_at(v):
        t = state["i"] * dt
        p = state["payout"]
        lifted = state["lifted"]  # 0 on deck .. 1 hanging
        if p >= IN_WATER:
            return WET_WEIGHT + WIRE_WEIGHT * p - DRAG * v * abs(v) + swell(t)
        # On deck or hanging in air; blend toward wet weight as it submerges.
        air = AIR_WEIGHT + 20 * math.sin(2 * math.pi * t / 4.0)
        hang = air + (WET_WEIGHT - AIR_WEIGHT) * max(0.0, p - 1.0) / (IN_WATER - 1.0) if p > 1.0 else air
        return lifted * hang + (1 - lifted) * abs(3 * math.sin(t))

    def idle(seconds):
        for _ in range(int(seconds * args.rate)):
            emit(0.0, tension_at(0.0))

    def lift(seconds, up):
        """Take the load (up) or set it down (not up) on deck."""
        n = int(seconds * args.rate)
        for k in range(n):
            state["lifted"] = (k + 1) / n if up else 1 - (k + 1) / n
            emit(0.0, tension_at(0.0))

    def move(target, vmax):
        """Trapezoidal wire move to target payout at up to vmax m/s."""
        sign = 1.0 if target > state["payout"] else -1.0
        v = 0.0
        while True:
            remaining = (target - state["payout"]) * sign
            if remaining <= 0.005:
                break
            # Accelerate, cruise, or brake so we stop at the target.
            v = min(vmax, v + args.accel * dt, math.sqrt(2 * args.accel * remaining))
            v = max(v, 0.02)
            step = min(v * dt, remaining)
            state["payout"] += sign * step
            emit(sign * v, tension_at(sign * v))
        state["payout"] = target
        emit(0.0, tension_at(0.0))

    cruise = args.speed / 60.0
    idle(args.deck_time)
    lift(6, up=True)              # take the weight
    move(8.0, 10 / 60.0)          # over the side and into the water, slowly
    move(args.max_payout, cruise) # lower
    idle(args.soak)               # bottles
    move(8.0, cruise)             # haul back
    move(0.0, 10 / 60.0)          # out of the water and onto the deck
    lift(6, up=False)             # set it down
    idle(args.deck_time)


if __name__ == "__main__":
    main()
