#!/usr/bin/env python3
"""Join tenant events and adjudicated outcomes into a labelled JSONL dataset."""
import argparse
import json
from pathlib import Path


LABELS = {"confirmed_fraud": True, "legitimate": False, "false_positive": False}


def rows(path):
    with Path(path).open(encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if line.strip():
                try:
                    yield json.loads(line)
                except json.JSONDecodeError as exc:
                    raise ValueError(f"{path}:{line_number}: invalid JSON: {exc}") from exc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("events", help="Canonical event JSONL, without labels")
    parser.add_argument("outcomes", help="Outcome ledger JSONL exported by the gateway")
    parser.add_argument("output", help="Output labelled JSONL path")
    parser.add_argument("--tenant", default="", help="Tenant for canonical event files that omit tenant IDs")
    args = parser.parse_args()

    events = {}
    for row in rows(args.events):
        event = row.get("event", row)
        tenant = row.get("tenant", event.get("tenant", args.tenant))
        key = (tenant, event.get("tenant_event_id", ""))
        if not key[1] or key in events:
            raise ValueError(f"event ID missing or duplicated: {key}")
        events[key] = event

    latest = {}
    for row in rows(args.outcomes):
        outcome = row.get("submission", row)
        label = outcome.get("label")
        if label not in (*LABELS, "unknown"):
            raise ValueError(f"unsupported label: {label!r}")
        key = (row.get("tenant", outcome.get("tenant", "")), outcome.get("tenant_event_id", ""))
        latest[key] = outcome

    destination = Path(args.output)
    destination.parent.mkdir(parents=True, exist_ok=True)
    joined = 0
    with destination.open("w", encoding="utf-8") as output:
        for key, outcome in sorted(latest.items()):
            label = outcome.get("label")
            if key not in events or label == "unknown":
                continue
            event = dict(events[key])
            event.pop("tenant", None)
            event["confirmed_fraud"] = LABELS[label]
            if outcome.get("loss_kes") is not None:
                event["outcome_loss_kes"] = float(outcome["loss_kes"])
            output.write(json.dumps(event, separators=(",", ":"), ensure_ascii=False) + "\n")
            joined += 1
    print(json.dumps({"events": len(events), "outcomes": len(latest), "labelled_rows": joined}))


if __name__ == "__main__":
    main()
