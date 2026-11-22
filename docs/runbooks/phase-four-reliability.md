# Phase 4 reliability runbook

## Service objectives

- Initial service target: 99.95% successful HTTP requests over a rolling 30-day window (about 21.6 minutes of error budget). Exclude `/metrics` scrapes. `/healthz` is process liveness; `/readyz` checks engine and tokenizer readiness.
- Assess target: p99 below 150 ms. The exported histogram observes completed decisions, not rejected or failed assessments; pair it with the HTTP error rate and degraded-decision rate.
- Alert rules and dashboard are in `deploy/monitoring/`. These targets are not evidence that the objective has been met; retain 90 days of monitoring history before making that claim.

## Triage

1. Check `/healthz`, `/readyz`, `kinga_http_requests_total`, assessment latency, degraded rate, and decision mix. Filter labels only by the bounded route, method, status, and decision; never add tenant IDs, event IDs, phone numbers, account references, tokens, or raw URLs as metric labels.
2. If engine readiness fails, inspect the Rust engine process, policy/model startup logs, CPU throttling, and model artifact/version. Roll back the last policy/model deployment if the change aligns with the incident.
3. If tokenizer readiness fails, stop consortium/tokenization traffic and preserve existing assess availability only under the institution's approved degraded-mode policy. Do not bypass tokenization by sending raw identifiers into the consortium.
4. If p99 breaches while dependencies are healthy, inspect saturation and queueing before adding capacity. The current JSONL stores are single-writer and are not safe for multiple gateway replicas sharing a volume.
5. If a region is unavailable, fail over only after the destination has the correct region-scoped secrets, policy/model versions, and durable data. Verify tenant residency constraints before restoring write traffic.
6. Record the timeline, customer impact, decisions affected, data exposure assessment, and follow-up actions in a postmortem. Notify members through the incident process in the signed consortium agreements.

## Deployment and rollback

- Keep engine policy and model artifacts versioned and immutable. Promote a canary only after readiness, smoke checks, and shadow-decision comparison.
- Roll back by restoring the previous image digest and policy/model versions. Do not edit a running policy file in place.
- Do not enable multiple gateway replicas until local JSONL ledgers and in-memory idempotency/features have been migrated to shared durable stores with concurrency control.

## Multi-region limits

The repository supplies instrumentation and operating procedures, not an active-active deployment. Regional failover requires a tested replication/ownership design for evidence, cases, outcomes, consortium state, idempotency, graph snapshots, and key material. Until then, operate one writer per dataset and use a documented restore/failover procedure.
