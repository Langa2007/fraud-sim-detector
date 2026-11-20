# ADR-004: Shadow, Advisory, and Enforce Modes

**Status:** Accepted for Phase 1

## Context
Deploying fraud interception directly into critical banking rails poses false-positive friction risks. Phase 1 must establish trust before blocking transactions.

## Decision
1. Implement a three-stage operational progression:
   - **Shadow (Phase 1):** Compute all scores and rules, write tamper-evident evidence, return `mode: shadow` and `decision: ALLOW` to transaction flow while returning internal alerts/recommendations.
   - **Advisory (Phase 2):** Send real-time alerts to the institution's fraud review team and case queue while maintaining transaction flow.
   - **Enforce (Phase 2+):** Directly return enforceable `ALLOW | STEP_UP | HOLD | BLOCK` actions with an instant kill-switch back to shadow mode.
2. The Rust decision core validates that Phase 1 policies strictly operate in `shadow` mode.

## Consequences
- Zero disruption to partner production transactions during initial backtesting and baseline discovery.
- Complete evidence generation allows comparison against ground-truth confirmed fraud labels.
