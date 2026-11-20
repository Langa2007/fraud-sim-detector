# ADR-005: Dependency Timeouts and Degraded Mode

**Status:** Accepted for Phase 1

## Context
Kinga evaluates signals within a 150 ms p99 latency budget. External dependencies like telco APIs can introduce unpredictable latency or outages.

## Decision
1. Impose a strict 60 ms timeout on telco signal lookups and 100 ms on internal engine RPC.
2. When a dependency times out or returns unavailable:
   - Mark `degraded: true` in the engine response and evidence record.
   - Fall back to local feature evaluation (velocity counters, device history, beneficiary first-seen, balance drain).
   - In Phase 1 shadow mode, fail-open to scoring with available features without blocking client requests.

## Consequences
- No external dependency outage can hang the decision path.
- Transparent telemetry via `degraded: true` enables tracking telco signal availability.
