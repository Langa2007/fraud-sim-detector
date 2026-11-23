#!/usr/bin/env python3
"""Convert canonical Phase 1 CSV columns into assessment JSONL for replay."""

import argparse
import csv
import json
import os
import sys
import tempfile


def value(row, key):
    """Return a trimmed CSV value, treating missing values as empty strings."""
    return (row.get(key) or "").strip()


def number(row, key, cast):
    """Parse an optional numeric value from a CSV row."""
    raw_value = value(row, key)
    return cast(raw_value) if raw_value else None


def make_event(row):
    """Convert one CSV row into a canonical event dictionary."""
    event = {
        "tenant_event_id": value(row, "tenant_event_id"),
        "event_type": value(row, "event_type"),
        "occurred_at": value(row, "occurred_at"),
        "channel": value(row, "channel"),
        "subject": {},
    }

    for key in ("msisdn", "account_ref", "customer_ref"):
        if value(row, key):
            event["subject"][key] = value(row, key)

    device = {
        key: value(row, key)
        for key in ("device_id", "ip", "app_version")
        if value(row, key)
    }
    if device:
        event["device"] = device

    transaction = {}
    amount = number(row, "amount", float)
    if amount is not None:
        transaction["amount"] = amount

    for key in ("currency", "rail"):
        if value(row, key):
            transaction[key] = value(row, key)

    beneficiary = {
        key: value(row, f"beneficiary_{key}")
        for key in ("type", "ref")
        if value(row, f"beneficiary_{key}")
    }
    if beneficiary:
        transaction["beneficiary"] = beneficiary
    if transaction:
        event["transaction"] = transaction

    signals = {}
    for key in (
        "sim_swap_age_minutes",
        "transfers_in_window",
        "new_beneficiaries_in_window",
        "window_minutes",
        "account_age_days",
        "inbound_fan_in",
    ):
        parsed = number(row, key, int)
        if parsed is not None:
            signals[key] = parsed

    parsed = number(row, "balance_drain_ratio", float)
    if parsed is not None:
        signals["balance_drain_ratio"] = parsed

    for key in ("device_first_seen", "beneficiary_first_seen", "rapid_outbound"):
        raw_value = value(row, key).lower()
        if raw_value:
            if raw_value not in ("true", "false", "1", "0", "yes", "no"):
                raise ValueError(f"{key} must be a boolean")
            signals[key] = raw_value in ("true", "1", "yes")

    if signals:
        event["signals"] = signals

    confirmed_fraud = value(row, "confirmed_fraud").lower()
    if confirmed_fraud in ("true", "1", "yes"):
        event["confirmed_fraud"] = True
    elif confirmed_fraud in ("false", "0", "no"):
        event["confirmed_fraud"] = False
    elif confirmed_fraud:
        raise ValueError("confirmed_fraud must be a boolean")

    return event


def read_rows(source):
    """Read CSV or Parquet input and return rows as dictionaries."""
    if source.lower().endswith((".parquet", ".pq")):
        try:
            import pyarrow.parquet as parquet
        except ImportError as error:
            print(
                "Parquet input requires pyarrow (python -m pip install pyarrow)",
                file=sys.stderr,
            )
            raise SystemExit(2) from error
        return parquet.read_table(source).to_pylist()

    with open(source, newline="", encoding="utf-8-sig") as csv_file:
        return list(csv.DictReader(csv_file))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("source")
    parser.add_argument("destination")
    args = parser.parse_args()

    rows = read_rows(args.source)
    directory = os.path.dirname(os.path.abspath(args.destination))
    file_descriptor, temporary_path = tempfile.mkstemp(
        prefix=".kinga-ingest-", dir=directory, text=True
    )

    try:
        with os.fdopen(file_descriptor, "w", encoding="utf-8") as output:
            for row_number, row in enumerate(rows, 2):
                try:
                    event = make_event(row)
                except Exception as error:
                    raise ValueError(f"row {row_number}: {error}") from error
                output.write(
                    json.dumps(event, separators=(",", ":"), ensure_ascii=False) + "\n"
                )
        os.replace(temporary_path, args.destination)
    except Exception:
        try:
            os.unlink(temporary_path)
        except FileNotFoundError:
            pass
        raise

    print(f"converted {len(rows)} rows to {args.destination}")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(error, file=sys.stderr)
        sys.exit(1)
