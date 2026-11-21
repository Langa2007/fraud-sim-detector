# ADR-007: Tokenisation Architecture, Consortium Privacy Floor, and Entity Graph Fusion

**Status:** Accepted for Phase 3

## Context
SIM-swap syndicates, mule account networks, and rapid float depletion schemes routinely operate across multiple institutional boundaries—transferring stolen funds from telco mobile wallets (e.g. M-PESA, Airtel Money) into bank accounts, digital lenders, and agent networks. Individual institutions only observe their local slice of the transaction trail.

However, sharing customer data across competing institutions is heavily restricted under the **Kenya Data Protection Act (DPA 2019)** and Central Bank of Kenya (CBK) guidelines. Raw identifiers (MSISDNs, bank account numbers, National IDs, device fingerprints) must never be shared in plaintext, aggregated into a centralized vulnerable database, or exposed to unauthorized parties. Furthermore, a single institution's unverified allegation of fraud must not be used to unilaterally blackball a citizen without independent multi-party corroboration.

## Decision

1. **Dedicated Rust Tokenizer Service (`services/tokenizer`):**
   - Implemented as a high-performance, hardened HTTP microservice in Rust.
   - Computes keyed HMAC-SHA256 tokens with strict domain separation:
     - `network` scope: `HMAC-SHA256(NetworkKey, "network:<namespace>:<value>")`
     - `tenant` scope: `HMAC-SHA256(NetworkKey, "tenant:<tenant_id>:<namespace>:<value>")`
   - Allowed namespaces: `msisdn`, `account`, `device`, `beneficiary`, `agent`, `wallet`.
   - Never logs, caches, or persists raw identifiers or token outputs (`Cache-Control: no-store`).
   - Supports key rotation via versioned keyrings (`active_version` + configurable historical versions).

2. **Tamper-Evident Hash-Chained Consortium Ledger (`services/gateway/internal/consortium`):**
   - Append-only journal recording indicator operations (`contribute`, `dispute`, `remove`, `reinstate`).
   - Every entry is sequentially numbered and SHA-256 hash-chained to the prior record (`Hash = SHA256(Seq + Action + Tenant + Indicator + PreviousHash)`).
   - Guarantees complete auditability and non-repudiation of member reports.

3. **Deterministic Privacy Floor ($k \ge 2$):**
   - An indicator token is **never** disclosed in query responses unless authenticated contributions exist from at least $k \ge 2$ independent, distinct member institutions.
   - Single-institution reports remain strictly private and unobservable to other network participants, preventing competitive intelligence gathering, single-point false positives, and weaponization of fraud reporting.
   - Member identity and contributor names are completely redacted from query responses.

4. **Exponential Confidence Decay & Time-to-Live (TTL):**
   - Indicator confidence decays continuously with a 30-day half-life:
     $$C(t) = \frac{1}{N} \sum_{i=1}^N C_{0,i} \times 2^{-\frac{\Delta t_i}{30 \times 24\text{h}}}$$
   - Indicators automatically expire upon reaching their bounded TTL (maximum 90 days), after which they immediately cease matching.

5. **Dispute & Governance Resolution Lifecycle:**
   - Any contributing member can submit an authenticated dispute (`POST /v1/consortium/indicators/{namespace}/{token}/disputes`) if customer KYC confirms false positive activity; disputed indicators immediately stop returning positive matches.
   - Governance administrators can resolve disputed indicators (`remove` or `reinstate`) with mandatory audited justification.

6. **Multi-Tenant Entity Graph Runtime (`services/gateway/internal/graph`):**
   - Weekly batch pipeline (`tools/graph/build_graph.py`) generates network community clusters and token-keyed features (`degree`, `inbound_transfers`, `outbound_transfers`, `independent_tenants`, `mule_likelihood`, `community_token`, version `kinga.graph.v1`).
   - Gateway loads graph snapshots atomically via lock-free atomic pointer swaps (`atomic.Pointer[snapshot]`), filtering queries against the independent-tenant privacy floor.

7. **Online Real-Time Assessment Fusion:**
   - Gateway fuses telco SIM-swap signals, local tenant velocity features, consortium beneficiary indicator hits, multi-tenant graph mule scores, and agent float movement anomalies before calling the Rust decision engine.
   - Decision engine rules (`beneficiary_consortium_hit`, `mule_graph_anomaly`, `agent_float_movement_anomaly`) trigger automated risk actions (`HOLD` or `STEP_UP`).

## Consequences

### Positive
- **Cross-Institution Immunity:** Syndicates can no longer hop across institutions to cash out without tripping multi-tenant indicators.
- **Privacy by Design:** Raw PII never leaves member premises in plaintext; only pseudonymous, keyed tokens are compared.
- **Fair Lending & Financial Inclusion:** The $k \ge 2$ privacy floor protects innocent consumers from erroneous or biased single-institution blacklisting.
- **Sub-Millisecond Lookup:** Gateway and Tokenizer operations complete with low overhead, preserving the 100ms assessment SLA.

### Trade-offs & Operational Requirements
- **Key Ceremony Governance:** Consortium network keys must be safeguarded with dual-control ceremonies and rotated in lockstep across members.
- **Batch Freshness:** Graph snapshots require scheduled weekly generation; zero-day mule accounts rely on consortium real-time indicator hits and agent float structuring rules until the next batch graph run.
