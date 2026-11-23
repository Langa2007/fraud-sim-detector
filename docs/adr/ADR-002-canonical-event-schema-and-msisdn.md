# ADR-002: Canonical Event Schema and MSISDN Normalisation

**Status:** Accepted for Phase 1

## Context
Kenyan mobile banking and payment rails represent phone numbers in disparate formats: `07XXXXXXXX`, `01XXXXXXXX`, `2547XXXXXXXX`, `+2547XXXXXXXX`, or international codes. Ingestion from batch CSV/Parquet drops and real-time APIs must map into a single canonical event schema (`schemas/events/assessment.schema.json`) before reaching the feature runtime or decision engine.

## Decision
1. Standardize all phone identifiers to the ITU-T E.164 international standard (`+` followed by country code and national number with no leading zeros).
2. Batch ingest normalises accepted Kenyan phone formats to E.164. The real-time Go gateway accepts E.164 only and rejects other formats with `422 Unprocessable Entity` (`msisdn_must_be_e164`).
3. Core event fields follow `schemas/events/assessment.schema.json`: `tenant_event_id`, `event_type`, RFC3339 `occurred_at` with an explicit offset, `channel`, `subject`, optional `device`/`transaction`, and optional `signals`.
4. Phase 1 transaction amounts are KES-only because decision thresholds and `amount_kes` use KES. `funds_transfer`, `loan_application`, and `agent_float_movement` require a numeric `transaction.amount` and `transaction.currency: "KES"`. Other currencies are rejected until an FX source, timestamp, conversion policy, and audit fields are specified.
5. Kenyan event producers preserve the `+03:00` offset in `occurred_at`; the overnight feature uses the local hour encoded by that offset. Model feature order and meaning are frozen by `schemas/features/v1.json`; incompatible changes require a new feature schema version.

## Consequences
- Every downstream service (feature runtime, telco connectors, tokenizers, evidence logger) receives E.164 without needing per-service regex sanitation.
- Malformed inputs are rejected immediately at the edge.
- Phase 1 risk scores cannot silently compare amounts denominated in different currencies; regional/multi-currency support remains a later versioned change.
