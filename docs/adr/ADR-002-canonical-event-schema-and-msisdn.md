# ADR-002: Canonical Event Schema and MSISDN Normalisation

**Status:** Accepted for Phase 1

## Context
Kenyan mobile banking and payment rails represent phone numbers in disparate formats: `07XXXXXXXX`, `01XXXXXXXX`, `2547XXXXXXXX`, `+2547XXXXXXXX`, or international codes. Ingestion from batch CSV/Parquet drops and real-time APIs must map into a single canonical event schema (`schemas/events/assessment.schema.json`) before reaching the feature runtime or decision engine.

## Decision
1. Standardize all phone identifiers to the ITU-T E.164 international standard (`+` followed by country code and national number with no leading zeros).
2. The Go gateway and ingest services enforce E.164 normalisation at the boundary; invalid formats trigger `422 Unprocessable Entity` (`msisdn_must_be_e164`).
3. Core event fields follow `schemas/events/assessment.schema.json`: `tenant_event_id`, `event_type`, `occurred_at`, `channel`, `subject`, `device`, `transaction`, and optional `signals`.

## Consequences
- Every downstream service (feature runtime, telco connectors, tokenizers, evidence logger) assumes clean E.164 without needing per-service regex sanitation.
- Malformed inputs are rejected immediately at the edge.
