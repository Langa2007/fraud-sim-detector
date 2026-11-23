#!/usr/bin/env python3
"""Generate canonical synthetic assessment events and separate labels.

All identifiers are synthetic references; the generator deliberately emits no
phone numbers, customer names, or other real-world identifiers.
"""

import argparse
import datetime as dt
import json
import random
from collections import Counter
from pathlib import Path


TYPOLOGIES = (
    "sim_swap_account_takeover",
    "beneficiary_velocity_abuse",
    "balance_drain",
    "young_account_fan_in_out",
    "agent_float_structuring",
)
LEGITIMATE = "legitimate"
DEFAULT_AS_OF = "2026-01-01T12:00:00+03:00"


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--count", type=int, default=1000, help="number of events (minimum 5)")
    parser.add_argument("--seed", type=int, default=7, help="random seed for reproducible output")
    parser.add_argument("--fraud-rate", type=float, default=0.06, help="target synthetic fraud share in [0, 1]")
    parser.add_argument("--as-of", default=DEFAULT_AS_OF, help="timezone-aware ISO-8601 reference timestamp")
    parser.add_argument("--out", default="synthetic.jsonl", help="canonical event JSONL output")
    parser.add_argument("--labels-out", help="label sidecar output (default: <out stem>.labels.jsonl)")
    return parser.parse_args()


def timestamp_for(rng, as_of, overnight):
    days_ago = rng.randint(1, 60)
    date = (as_of - dt.timedelta(days=days_ago)).date()
    if overnight:
        hour = rng.choice((*range(0, 5), 22, 23))
    else:
        hour = rng.randint(5, 21)
    local_time = dt.time(hour, rng.randint(0, 59), rng.randint(0, 59))
    return dt.datetime.combine(date, local_time, tzinfo=as_of.tzinfo).isoformat()


def event_for(index, typology, rng, as_of, mule_group):
    overnight = rng.random() < 0.25
    event_type = "agent_float_movement" if typology == "agent_float_structuring" else "funds_transfer"
    channel = "agent" if event_type == "agent_float_movement" else rng.choice(("mobile_app", "ussd", "web"))
    account_ref = f"synthetic-account-{index:08d}"
    if typology == "young_account_fan_in_out":
        account_ref = f"synthetic-mule-account-{mule_group:05d}"

    signals = {
        "device_first_seen": rng.random() < 0.03,
        "beneficiary_first_seen": rng.random() < 0.08,
        "balance_drain_ratio": round(rng.uniform(0.01, 0.35), 3),
        "transfers_in_window": rng.randint(0, 2),
        "new_beneficiaries_in_window": rng.randint(0, 1),
        "window_minutes": rng.randint(5, 60),
        "account_age_days": rng.randint(90, 3650),
        "inbound_fan_in": rng.randint(0, 3),
        "rapid_outbound": False,
        "agent_float_anomaly": False,
    }
    amount = round(rng.uniform(200, 250000), 2)

    if typology == "sim_swap_account_takeover":
        signals.update({
            "sim_swap_age_minutes": rng.randint(1, 1439),
            "device_first_seen": True,
            "beneficiary_first_seen": True,
        })
        amount = round(rng.uniform(50000, 250000), 2)
    elif typology == "beneficiary_velocity_abuse":
        signals.update({
            "transfers_in_window": rng.randint(3, 7),
            "new_beneficiaries_in_window": rng.randint(2, 5),
            "window_minutes": rng.randint(2, 15),
        })
    elif typology == "balance_drain":
        signals["balance_drain_ratio"] = round(rng.uniform(0.70, 0.99), 3)
    elif typology == "young_account_fan_in_out":
        signals.update({
            "account_age_days": rng.randint(1, 29),
            "inbound_fan_in": rng.randint(5, 15),
            "rapid_outbound": True,
        })
    elif typology == "agent_float_structuring":
        signals.update({
            "transfers_in_window": rng.randint(3, 8),
            "new_beneficiaries_in_window": rng.randint(2, 5),
            "window_minutes": rng.randint(2, 15),
            "agent_float_anomaly": True,
        })

    return {
        "tenant_event_id": f"synth-{index:08d}",
        "event_type": event_type,
        "occurred_at": timestamp_for(rng, as_of, overnight),
        "channel": channel,
        "subject": {"account_ref": account_ref},
        "device": {"device_id": f"synthetic-device-{rng.randint(1, 5000):05d}", "app_version": "synthetic-1"},
        "transaction": {
            "amount": amount,
            "currency": "KES",
            "rail": rng.choice(("mpesa", "pesalink", "bank_transfer")),
            "beneficiary": {
                "type": "agent_float" if event_type == "agent_float_movement" else "wallet",
                "ref": f"synthetic-beneficiary-{rng.randint(1, 3000):05d}",
            },
        },
        "signals": signals,
    }


def main():
    args = parse_args()
    if args.count < 5:
        raise SystemExit("--count must be at least 5 so the generator can cover all typologies")
    if not 0 <= args.fraud_rate <= 1:
        raise SystemExit("--fraud-rate must be between 0 and 1")
    try:
        as_of = dt.datetime.fromisoformat(args.as_of)
    except ValueError as exc:
        raise SystemExit(f"invalid --as-of timestamp: {exc}") from exc
    if as_of.tzinfo is None or as_of.utcoffset() is None:
        raise SystemExit("--as-of must include a timezone offset")

    event_path = Path(args.out)
    labels_path = Path(args.labels_out) if args.labels_out else event_path.with_name(f"{event_path.stem}.labels.jsonl")
    if event_path.resolve() == labels_path.resolve():
        raise SystemExit("event output and label sidecar must be different files")
    event_path.parent.mkdir(parents=True, exist_ok=True)
    labels_path.parent.mkdir(parents=True, exist_ok=True)

    rng = random.Random(args.seed)
    fraud_count = 0 if args.fraud_rate == 0 else min(args.count, max(5, round(args.count * args.fraud_rate)))
    fraud_types = [TYPOLOGIES[i % len(TYPOLOGIES)] for i in range(fraud_count)]
    rng.shuffle(fraud_types)
    labels = [LEGITIMATE] * (args.count - fraud_count) + fraud_types
    rng.shuffle(labels)

    counts = Counter(labels)
    mule_group = 0
    with event_path.open("w", encoding="utf-8", newline="\n") as events, labels_path.open(
        "w", encoding="utf-8", newline="\n"
    ) as sidecar:
        for index, typology in enumerate(labels):
            group = mule_group // 5
            event = event_for(index, typology, rng, as_of, group)
            if typology == "young_account_fan_in_out":
                mule_group += 1
            events.write(json.dumps(event, separators=(",", ":"), sort_keys=True) + "\n")
            label = {
                "tenant_event_id": event["tenant_event_id"],
                "confirmed_fraud": typology != LEGITIMATE,
                "typology": typology,
            }
            sidecar.write(json.dumps(label, separators=(",", ":"), sort_keys=True) + "\n")

    print(json.dumps({
        "events": args.count,
        "fraud_rate_target": args.fraud_rate,
        "fraud_events": fraud_count,
        "typologies": dict(sorted(counts.items())),
        "seed": args.seed,
        "as_of": as_of.isoformat(),
        "events_path": str(event_path),
        "labels_path": str(labels_path),
    }, sort_keys=True))


if __name__ == "__main__":
    main()
