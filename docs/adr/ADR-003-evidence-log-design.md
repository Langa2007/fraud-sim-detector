# ADR-003: Evidence-Log Design (Hash Chaining, Anchoring, Retention)

**Status:** Accepted for Phase 1

## Context
Under the 2026 Kenyan High Court precedent (Machakos SIM-swap ruling) and forthcoming CBK digital-fraud compensation frameworks, institutions must prove what they knew, when they knew it, and what policy decided.

## Decision
1. Maintain an append-only, tamper-evident hash-chained log (`data/evidence.jsonl`).
2. Each record contains:
   - Contiguous sequence number ($N$)
   - Deterministic evidence ID (`ev_{sequence}_{hash}`)
   - Tenant identifier and `tenant_event_id`
   - Timestamp in UTC
   - SHA-256 of the raw event
   - Evaluated feature snapshot
   - Resolved decision object
   - Previous record's hash ($H_{N-1}$)
   - Self hash ($H_N = \text{SHA256}(\text{canonical}(\text{Record}))$ with empty hash field)
3. Any corruption, omission, or edit breaks the chain and is detected during startup or via `GET /v1/evidence/verify` and `tools/evidence-verify`.

## Consequences
- Records cannot be altered retroactively without invalidating the subsequent chain.
- Phase 1 uses local file storage; Phase 4 will introduce periodic remote anchoring to external public or consortium ledgers.
