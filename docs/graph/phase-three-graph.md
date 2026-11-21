# Phase 3 graph batch

`tools/graph/build_graph.py` consumes JSONL from a trusted local adapter. Every entity identifier must already be a lowercase 64-character HMAC token from the Rust tokenizer. Rows may contain only `tenant`, `occurred_at`, `event_type`, `amount_kes`, and token fields `subject`, `device`, `sim`, `beneficiary`, `account`, `agent`, `wallet`. It rejects extra fields to prevent accidental PII ingestion.

The job bounds processing to a configurable recent window, emits token-level degree, directional transfer counts, distinct tenant count, a mule-likelihood heuristic, and a connected-community token. It writes output atomically with mode 0600 and does not persist row-level edges. Treat the output as pseudonymous personal data: access control and retention still apply.

Example:

```sh
python tools/graph/build_graph.py \
  --input data/tokenized-events.jsonl \
  --output data/graph-features.jsonl \
  --max-age-days 90 \
  --min-fan-in 3
```

This is an initial batch feature pipeline, not validated community detection or a production online graph service. Calibrate thresholds against member-provided labels and compare graph lift with the Phase 2 baseline before using features in decisions.
