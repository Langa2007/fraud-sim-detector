# Phase 2 model and outcome loop

## Label submission

`POST /v1/outcomes` uses the gateway's existing HMAC signature scheme and tenant key. The body is capped at 16 KiB. `Idempotency-Key` must equal `submission_id`; reusing that ID with a different body returns `409`. `observed_at` is when the reviewer established the label, while `recorded_at` is server time.

```json
{
  "submission_id": "review-2026-000042",
  "tenant_event_id": "evt-000042",
  "evidence_id": "ev_42_0123456789abcdef",
  "label": "confirmed_fraud",
  "reviewer": "reviewer-17",
  "notes": "Institution-confirmed account takeover",
  "loss_kes": 12500,
  "observed_at": "2026-11-20T10:30:00+03:00"
}
```

Allowed labels are `confirmed_fraud`, `false_positive`, `legitimate`, and `unknown`. Reviewer identity is mandatory. Notes are limited to 512 characters; do not put unnecessary personal data in them. `false_positive` and `legitimate` are negative training labels; `unknown` is excluded. Outcomes append to `KINGA_OUTCOME_PATH` (default `data/outcomes.jsonl`), with restrictive file permissions and a SHA-256 hash chain. The ledger is local single-writer storage for this development phase; production multi-instance use needs a transactional durable store.

The model-preparation command expects an event JSONL export and this outcome ledger. It joins by tenant plus `tenant_event_id`, uses the latest ledger row for each event, discards unknown or unmatched labels, and emits canonical event rows with `confirmed_fraud` booleans:

```sh
python3 tools/ml/prepare_dataset.py events.jsonl data/outcomes.jsonl data/labelled.jsonl --tenant approved-tenant
```

## Train, evaluate, and export

Create an isolated environment, install `requirements-ml.txt`, and provide authorized, de-identified, tenant-specific data with confirmed labels:

```sh
python3 -m pip install -r requirements-ml.txt
python3 tools/ml/train.py data/labelled.jsonl --out models/current --max-fpr 0.005
```

The trainer sorts by event time and uses the earliest 60% to fit a gradient-boosted tree, the next 20% for Platt calibration and a decision threshold, and the latest 20% as one held-out evaluation window. It reports recall, precision, false-positive rate, PR-AUC, KSh-weighted miss/friction costs (using reviewed loss when supplied, otherwise transaction amount), channel and event-type slices, and per-feature population stability index. Threshold selection never reads the held-out labels. Small slices are marked `insufficient_labels` rather than treated as evidence of fairness.

The `current` directory receives:

- `model.onnx`: classifier converted for the Rust inference runtime.
- `model.json`: model version, exact feature order, calibration parameters, and operating threshold.
- `evaluation.json`: held-out metrics and evaluation limitations.

For Compose, set `KINGA_MODEL_PATH=/app/models/current/model.onnx`. The repository's `./models` directory is mounted read-only at `/app/models`. The engine rejects an invalid model card or incompatible feature list at startup. Without a configured model, rules continue to run. With a model configured but an inference error, the engine keeps the rules decision and marks the response `degraded`.

## Safety and interpretation

The runtime combines model probability with the rules score using the higher score and keeps policy rules as minimum action guardrails. It adds `MODEL_HIGH_RISK` when the calibrated probability reaches the exported operating threshold. It remains `mode: shadow`; this phase does not enable transaction blocking. One model is loaded per engine process, so tenant-specific reports require separate training/export and deployment boundaries. Never treat synthetic-data metrics as evidence of partner performance or turn on enforcement based on the included generator.

The current outcome store has no correction endpoint: submit a new `submission_id` for a review correction. The preparation script chooses the latest record in ledger order. Reviewer identity is a tenant-supplied audit string and must be mapped to the institution's authenticated reviewer identity by its integration. Historical event export and outcome export must be retained with tenant approval and access controls.

## Case review

Every non-`ALLOW` recommendation opens one tenant-scoped case with the decision, reason codes, policy/model versions and evidence ID. `GET /v1/cases?status=open&limit=50` lists the tenant's latest cases. A signed `POST /v1/cases/{caseID}/actions` records `assign`, `note`, `release`, or `hold` actions in an append-only hash-chained local ledger. Release and hold dispositions require a reason. Its `Idempotency-Key` must equal the action's `action_id`. `GET /v1/cases/{caseID}/evidence` returns a JSON export containing the case history and the original evidence record after verifying the complete evidence chain.

Case `hold` and `release` actions are review dispositions only; they do not control a transaction. The reviewer field is supplied by the tenant integration and must be mapped to the institution's authenticated identity. Ledgers are single-writer local files and must be moved to a durable transactional store before multi-replica operation. The UI, assignment SLAs, escalations and PDF export remain to be built.
