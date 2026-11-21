# Phase 3 consortium governance pack (draft)

This pack is an implementation aid, not a signed charter, data-sharing agreement, DPIA, or legal opinion. Production contributions and cross-member lookup must remain disabled until counsel and each member approve the applicable documents and controls.

## Decision record

- Network token scope: HMAC-SHA-256 with a dedicated network key and domain separation by entity namespace. Tenant scope uses the same key with a tenant-specific domain. Raw identifiers must never be persisted in consortium storage, graph output, logs, or evidence exports.
- Shared indicator payload: namespace, network token, confidence, expiry, status. Tenant identity and member identity are withheld from lookup responses. Contribution events remain in a restricted, hash-chained ledger for governance and audit.
- Query disclosure: no hit is returned until the configured minimum of two distinct members has contributed. This is an engineering privacy floor, not a legal determination that the sharing is anonymous.
- Retention: indicator TTL is bounded to 90 days in the API. Expiry removes eligibility from query immediately; physical ledger retention and deletion schedules require member/legal approval.
- Disputes: a contributing member may dispute an indicator; disputed indicators stop matching immediately. A separately controlled governance credential can remove or reinstate the indicator with a reason recorded in the ledger.
- Contributions: one contribution per tenant and entity token. Contribution counts are available only to the authenticated member itself.

## Required member onboarding checklist

- [ ] Member entity and authorized operators verified; tenant key issued and stored in approved secret management.
- [ ] Data-sharing purpose, lawful basis, controller/processor roles, data categories, retention, data-subject request handling, and cross-border transfers documented.
- [ ] Security architecture, threat model, DPIA, and member-specific data-flow diagram reviewed by counsel and the privacy lead.
- [ ] Incident contacts and 24-hour escalation route exchanged; tabletop exercise scheduled.
- [ ] Contribution and query entitlement, minimum confirmation threshold, dispute SLA, removal criteria, audit rights, and exit/deletion procedure signed.
- [ ] Token-key ceremony documented with named custodians, dual control, backup, rotation schedule, compromise procedure, and re-tokenization plan.
- [ ] Sandbox replay and false-positive review completed before production access.

## Operational workflows

### Indicator lifecycle

1. Member normalizes the identifier locally, then requests a network-scoped token from the Rust tokenizer over the private service network.
2. Member submits only that token, a calibrated confidence value, and a bounded TTL using its signed tenant credential.
3. Gateway appends a chained contribution record. Independent-member count is based on distinct authenticated tenants, not repeated submissions.
4. Query returns only after the configured threshold is met. Confidence decays with a 30-day half-life and the indicator stops matching at expiry.
5. A contributing member can dispute the indicator. Governance reviews the dispute outside the API and records removal or reinstatement with a reason.

### Key rotation

The tokenizer defaults to the active key version and can emit a configured prior version when `key_version` is explicitly requested for a controlled migration. During rotation, operators must dual-tokenize new and still-live identifiers under old and new versions, migrate or expire outstanding indicators, and prevent mixed-version queries from counting as independent evidence. The service does not automate alias migration; do not rotate a live network key without a rehearsed migration and member notice.

### Incident response

1. Revoke the affected caller or network key, pause contribution/query access, and preserve relevant ledger and service audit records.
2. Notify the consortium incident lead and affected members through the agreed secure contact tree.
3. Determine whether raw identifiers, keys, or linkable tokens were exposed; engage each member's privacy and legal contacts for statutory notification decisions.
4. Rotate or retire affected keys under dual control, reconcile indicator integrity, and document corrective actions before restoring access.

## Current implementation boundary

The repository has a Rust tokenizer, Go signed routes and append-only indicator ledger, minimum-confirmation lookup, TTL confidence decay, disputes and governance resolution, member contribution counts, and a Python batch graph feature builder. It does not implement production key management/HSM integration, automatic graph artifact serving, PSI, differential privacy, data-subject request automation, or signed member/legal artifacts. The local JSONL ledger is a single-writer pilot store and must not be treated as a multi-node production database.

## Gate evidence still required

- Signed consortium charter and data-sharing agreements, plus production legal sign-off.
- At least five active members of mixed institution types with verified contribution activity.
- Replay and live evaluation demonstrating cross-institution saves and lift over the Phase 2 model-only baseline, with agreed false-positive costs.
- Exercised dispute/removal workflow, incident tabletop, and evidence of zero material data-protection incidents.
- Published privacy/security whitepaper reviewed by the participating members.
