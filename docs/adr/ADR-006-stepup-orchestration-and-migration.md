# ADR-006: Out-of-Band Step-Up Orchestration and Staged Enforce Migration

**Status:** Accepted for Phase 2

## Context
Phase 2 introduces hybrid model scoring (gradient boosted trees via ONNX alongside rules guardrails) and requires closing the loop on real fraud events. When high-risk transactions are detected, institutions require out-of-band step-up verification before holding or blocking funds. Moving from shadow to enforce also carries operational friction risk; institutions need deterministic staged rollouts and emergency kill switches.

## Decision
1. **Out-of-Band Step-Up Orchestration (`services/gateway/internal/stepup`):**
   - Provide multi-channel challenge dispatch: USSD Push (`*254#`), SMS callback, and verified in-app challenges.
   - Support localized Swahili (`sw`) and English (`en`) prompt templates.
   - Enforce timeouts (2 minutes default for USSD) and channel escalation (e.g. USSD failure escalates to SMS callback).
2. **Staged Rollout & Emergency Kill Switch (`services/gateway/internal/migration`):**
   - Implement deterministic hash bucketing across tenant segments: 0% (Shadow), 5% (Canary Advisory), 25% (Controlled Enforce), 100% (Full Enforce).
   - Instant operator kill-switch to immediately fall back to shadow mode.
   - Automated friction spike guard: if rolling false-positive friction exceeds the configured ceiling (default 1.5%), the engine automatically trips emergency rollback to shadow mode.
3. **One-Click Evidence Packs:**
   - Expose `GET /v1/cases/{caseID}/evidence/pack` generating a printable, self-contained HTML document with SHA-256 chain verification seal, transaction details, reason weights, and review audit history.

## Consequences
- Institutions can safely trial active step-up and enforcement with automated circuit breakers against false-positive customer friction.
- Audited labels from `/v1/outcomes` cleanly feed into the weekly ONNX retraining pipeline.
