#!/usr/bin/env python3
"""Replay canonical assessment JSONL and report catch/friction by outcome."""

import argparse
import hashlib
import hmac
import json
import time
import urllib.request


def load_labels(path):
    """Read and validate the optional outcome sidecar keyed by event ID."""
    if not path:
        return {}

    labels = {}
    with open(path, encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if not line.strip():
                continue

            row = json.loads(line)
            event_id = row.get("tenant_event_id")
            if (
                not event_id
                or event_id in labels
                or not isinstance(row.get("confirmed_fraud"), bool)
            ):
                raise ValueError(
                    f"{path}:{line_number}: missing/duplicate event ID "
                    "or invalid fraud label"
                )
            labels[event_id] = row

    return labels


def percentile(values, quantile):
    """Return a nearest-rank percentile, or None when there are no values."""
    if not values:
        return None

    ordered = sorted(values)
    index = min(len(ordered) - 1, int((len(ordered) - 1) * quantile))
    return round(ordered[index], 2)


def add_outcome(counts, label, flagged):
    """Increment the confusion-matrix cell for one assessed event."""
    if label and flagged:
        counts["tp"] += 1
    elif label:
        counts["fn"] += 1
    elif flagged:
        counts["fp"] += 1
    else:
        counts["tn"] += 1


def rates(counts):
    """Calculate recall and false-positive rate for a confusion matrix."""
    positives = counts["tp"] + counts["fn"]
    negatives = counts["fp"] + counts["tn"]
    return {
        "recall": counts["tp"] / positives if positives else None,
        "false_positive_rate": counts["fp"] / negatives if negatives else None,
    }


def assess_event(row, args):
    """Send one signed assessment request and return its result and latency."""
    body = json.dumps(row, separators=(",", ":"), ensure_ascii=False).encode()
    timestamp = str(int(time.time()))
    signature = hmac.new(
        args.secret.encode(),
        timestamp.encode() + b"." + body,
        hashlib.sha256,
    ).hexdigest()
    request = urllib.request.Request(
        args.url,
        data=body,
        headers={
            "Content-Type": "application/json",
            "X-Kinga-Key": args.key,
            "X-Kinga-Timestamp": timestamp,
            "X-Kinga-Signature": signature,
            "Idempotency-Key": row["tenant_event_id"],
        },
    )

    start = time.perf_counter()
    with urllib.request.urlopen(request, timeout=5) as response:
        result = json.load(response)
    elapsed_ms = (time.perf_counter() - start) * 1000
    return result, elapsed_ms


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("events")
    parser.add_argument("--labels", help="optional synthetic label sidecar JSONL")
    parser.add_argument("--url", default="http://localhost:8080/v1/assess")
    parser.add_argument("--tenant", default="demo")
    parser.add_argument("--key", default="demo-key")
    parser.add_argument("--secret", default="demo-secret")
    args = parser.parse_args()

    counts = {"tp": 0, "fp": 0, "fn": 0, "tn": 0}
    errors = 0
    latencies = []
    by_typology = {}
    labels = load_labels(args.labels)
    seen = set()

    with open(args.events, encoding="utf-8") as events_file:
        for line in events_file:
            row = json.loads(line)
            event_id = row.get("tenant_event_id")
            if not event_id or event_id in seen:
                raise ValueError(f"missing or duplicate tenant_event_id: {event_id!r}")
            seen.add(event_id)

            if args.labels and event_id not in labels:
                raise ValueError(f"{args.labels}: label missing for event {event_id}")

            sidecar = labels.get(event_id, {})
            label = sidecar.get("confirmed_fraud", bool(row.pop("confirmed_fraud", False)))
            typology = sidecar.get("typology", "fraud" if label else "legitimate")
            typology_counts = by_typology.setdefault(
                typology, {"tp": 0, "fp": 0, "fn": 0, "tn": 0}
            )

            try:
                result, elapsed_ms = assess_event(row, args)
                latencies.append(elapsed_ms)
                flagged = result["decision"] != "ALLOW"
                add_outcome(counts, label, flagged)
                add_outcome(typology_counts, label, flagged)
            except Exception as error:
                errors += 1
                print(f"error {event_id}: {error}")

    total = sum(counts.values())
    typology_results = {}
    for typology, typology_counts in by_typology.items():
        typology_results[typology] = {**typology_counts, **rates(typology_counts)}

    report = {
        "events": total + errors,
        "errors": errors,
        **rates(counts),
        "precision": counts["tp"] / (counts["tp"] + counts["fp"])
        if counts["tp"] + counts["fp"]
        else None,
        "latency_ms": {
            "p50": percentile(latencies, 0.5),
            "p95": percentile(latencies, 0.95),
            "p99": percentile(latencies, 0.99),
        },
        "confusion": counts,
        "typologies": typology_results,
    }
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
