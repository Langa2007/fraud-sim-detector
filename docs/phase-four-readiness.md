# Phase 4 readiness and implementation record

## Engineering work in this phase

- Added bounded-cardinality HTTP request metrics and a cumulative assessment-latency histogram; Prometheus recording/alert rules and a Grafana dashboard are in `deploy/monitoring/`.
- Added graceful gateway shutdown, ordered multi-provider telco failover, signed evidence-chain anchors, an on-prem Rust engine deployment guide, and thin Go/Python/JavaScript SDKs.
- Added an initial incident and SLO runbook plus a private vulnerability reporting policy.

## Work that still needs infrastructure or external parties

- Multi-AZ/active-active: not safe with the current local JSONL ledgers, in-memory idempotency/feature state, and graph snapshot. Use one writer until those are moved to shared stores with transactional ownership and a tested replication plan.
- Event bus and ClickHouse: not connected to the request path. Choose retention, event minimization, and tenant isolation first; do not stream raw request payloads by default.
- CAMARA providers: the current adapter handles the CAMARA SIM-swap retrieve-date shape. Airtel/Telkom endpoints can be configured only when they expose that compatible contract; provider-specific OAuth, consent, device-swap, number verification, and location verification adapters need operator specifications and credentials.
- Certification: the repo prepares operations and vulnerability reporting but contains no independent pen-test, closed findings, ISO 27001/SOC 2 audit, or compliance attestation.
- On-prem: a self-hostable Rust daemon and instructions exist; a customer deployment, signed release process, support SLA, and stable library/gRPC interface remain open.
- Regional expansion: event validation checks a three-letter uppercase currency format, but does not verify the ISO 4217 registry. The current model feature is KES-denominated. Do not score UGX/TZS amounts with a KES model; regional policy/model packs require registry validation, local labelled data, currency-aware feature contracts, and local legal review.
- 90-day SLO attainment, revenue coverage, and a second revenue segment are business/operational facts and cannot be asserted by this code change.

Phase 4 exit gates remain unchecked until their evidence exists. The monitoring configuration expresses targets; it is not proof they have been met.
