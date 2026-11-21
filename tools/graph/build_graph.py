#!/usr/bin/env python3
"""Build privacy-safe graph features from JSONL rows containing tokenized IDs only.

Input rows are expected to be prepared by an institution's trusted adapter. This
tool deliberately has no raw identifier fields and writes no row-level edges.
"""
from __future__ import annotations

import argparse
import json
import os
import tempfile
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
from pathlib import Path

TOKEN_KEYS = ("subject", "device", "sim", "beneficiary", "account", "agent", "wallet")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path, help="JSONL with tokenized entities")
    parser.add_argument("--output", required=True, type=Path, help="Atomic JSONL graph feature output")
    parser.add_argument("--min-fan-in", type=int, default=3)
    parser.add_argument("--max-age-days", type=int, default=90)
    return parser.parse_args()


def token(value: object) -> str:
    if not isinstance(value, str) or len(value) != 64 or any(c not in "0123456789abcdef" for c in value):
        raise ValueError("entity values must be lowercase 64-character HMAC tokens")
    return value


def build(rows: list[dict], min_fan_in: int, max_age_days: int) -> list[dict]:
    # token -> distinct counterparties, directional transfer counts and tenants
    peers: dict[tuple[str, str], set[tuple[str, str]]] = defaultdict(set)
    tenants: dict[tuple[str, str], set[str]] = defaultdict(set)
    inbound: Counter[tuple[str, str]] = Counter()
    outbound: Counter[tuple[str, str]] = Counter()
    seen: Counter[tuple[str, str]] = Counter()
    edge_types: dict[tuple[str, str], Counter[str]] = defaultdict(Counter)
    cutoff = datetime.now(timezone.utc) - timedelta(days=max_age_days)
    for row in rows:
        if set(row) - {"tenant", "occurred_at", "event_type", "amount_kes", *TOKEN_KEYS}:
            raise ValueError("input contains unsupported fields; strip PII before graph processing")
        tenant = row.get("tenant")
        if not isinstance(tenant, str) or not tenant:
            raise ValueError("tenant is required")
        timestamp = row.get("occurred_at")
        if not isinstance(timestamp, str):
            raise ValueError("occurred_at must be an ISO-8601 timestamp")
        try:
            occurred = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
            if occurred.tzinfo is None:
                raise ValueError("timezone required")
        except ValueError as exc:
            raise ValueError("occurred_at must be an ISO-8601 timestamp with timezone") from exc
        if occurred.astimezone(timezone.utc) < cutoff:
            continue
        nodes = [(key, token(row[key])) for key in TOKEN_KEYS if row.get(key) is not None]
        if len(nodes) < 2:
            continue
        for kind, value in nodes:
            node = (kind, value)
            seen[node] += 1
            tenants[node].add(tenant)
        source = next((value for kind, value in nodes if kind in ("subject", "account", "agent", "wallet")), nodes[0][1])
        target = next((value for kind, value in nodes if kind in ("beneficiary", "account", "wallet") and value != source), None)
        if target:
            source_node, target_node = ("subject", source), ("beneficiary", target)
            peers[source_node].add(target_node)
            peers[target_node].add(source_node)
            edge_types[source_node]["out"] += 1
            edge_types[target_node]["in"] += 1
            outbound[source_node] += 1
            inbound[target_node] += 1
        # Typed co-occurrence links create a bounded entity graph without storing edges.
        for i, (left_kind, left) in enumerate(nodes):
            for right_kind, right in nodes[i + 1:]:
                left_node, right_node = (left_kind, left), (right_kind, right)
                peers[left_node].add(right_node)
                peers[right_node].add(left_node)
    # Connected components provide stable, batch-local community identifiers.
    graph: dict[tuple[str, str], set[tuple[str, str]]] = defaultdict(set)
    for node, neighbours in peers.items():
        graph[node].update(neighbours)
        for neighbour in neighbours:
            graph[neighbour].add(node)
    communities: dict[tuple[str, str], str] = {}
    for start in graph:
        if start in communities:
            continue
        stack, members = [start], []
        while stack:
            current = stack.pop()
            if current in communities:
                continue
            communities[current] = "pending"
            members.append(current)
            stack.extend(graph[current] - set(communities))
        community_id = min(value for _, value in members)
        for member in members:
            communities[member] = community_id
    features = []
    for node, neighbours in peers.items():
        degree = len(neighbours)
        in_count, out_count = inbound[node], outbound[node]
        fan_in_score = min(1.0, in_count / max(min_fan_in, 1))
        rapid_out_score = min(1.0, out_count / max(in_count, 1)) if in_count else 0.0
        mule_score = min(1.0, 0.65 * fan_in_score + 0.35 * rapid_out_score) if in_count >= min_fan_in else 0.0
        features.append({
            "namespace": node[0], "token": node[1], "window_days": max_age_days,
            "degree": degree, "inbound_transfers": in_count, "outbound_transfers": out_count,
            "independent_tenants": len(tenants[node]), "mule_likelihood": round(mule_score, 6),
            "community_token": communities.get(node, node[1]),
            "version": "kinga.graph.v1",
        })
    return sorted(features, key=lambda item: (item["namespace"], item["token"]))


def main() -> None:
    args = parse_args()
    if args.min_fan_in < 2 or not 1 <= args.max_age_days <= 365:
        raise SystemExit("min-fan-in must be >=2 and max-age-days must be 1..365")
    rows = []
    with args.input.open(encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if line.strip():
                try:
                    row = json.loads(line)
                    if not isinstance(row, dict):
                        raise ValueError("row must be an object")
                    rows.append(row)
                except (json.JSONDecodeError, ValueError) as exc:
                    raise SystemExit(f"input line {line_number}: {exc}") from exc
    output = build(rows, args.min_fan_in, args.max_age_days)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=".graph-", dir=args.output.parent, text=True)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            for item in output:
                out.write(json.dumps(item, separators=(",", ":"), sort_keys=True) + "\n")
            out.flush()
            os.fsync(out.fileno())
        os.replace(temp_name, args.output)
    finally:
        if os.path.exists(temp_name):
            os.unlink(temp_name)


if __name__ == "__main__":
    main()
