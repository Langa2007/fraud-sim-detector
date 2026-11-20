# ADR-001: Phase 1 runtime and shadow boundary

**Status:** Accepted for Phase 1

## Context
The repository is a greenfield specification. Phase 1 requires a reproducible assessment contract, deterministic policy-driven rules and evidence across the planned Go and Rust lanes.

## Decision
Use Go for the public gateway, local feature runtime and telco connector interface; use Rust for policy evaluation and decision resolution. The gateway calls the engine through a private versioned HTTP/JSON contract (`POST /internal/v1/assess`) with a 100 ms deadline. Keep the canonical event and decision schema explicit. The policy loader accepts shadow mode only; returned actions are recommendations. Tenant-scoped API key plus HMAC request signatures and an append-only local hash chain protect the development path. The current telco implementation is a fixture-backed simulator; a live MNO adapter remains separate.

## Consequences
- The gateway and decision engine are separate processes and can be deployed independently.
- A connector interface keeps simulator behavior replaceable; the local feature runtime derives first-seen and transfer-velocity signals before updating hashed tenant history.
- Local development requires Go and Rust; Compose builds both service images.
- Local evidence is durable and tamper-evident against edits, but does not provide a remote immutable anchor or distributed writer safety.
- Idempotency is process-local and restarts clear its cache; durable idempotency requires the planned Redis/Postgres store.
- No production deployment is implied. Partner data, key management, legal review, Redis/Postgres/NATS and real telco integrations remain deployment gates.
- The internal JSON contract is a development boundary; migrate it to the planned protobuf/gRPC contract before multi-service production rollout.
