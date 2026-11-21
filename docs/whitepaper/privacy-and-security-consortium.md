# Kinga Privacy and Security Whitepaper: Consortium Intelligence & Cryptographic Tokenization

**Document Version:** 1.0.0  
**Target Audience:** Chief Information Security Officers (CISOs), Data Protection Officers (DPOs), General Counsel, and Technical Architecture Committees across Participating Licensed Financial Institutions and Telecommunications Operators.

---

## 1. Executive Summary & Regulatory Foundation

The modern fraud landscape in Kenya and the broader East African digital financial ecosystem is fundamentally multi-institutional. Fraud syndicates exploit the speed and interoperability of the National Payment System—utilizing unauthorized SIM swaps, social engineering, and collusive banking agents to funnel funds across mobile network operators (Safaricom M-PESA, Airtel Money) and commercial banks within minutes.

Individual institutions operating in isolation are systematically blind to cross-rail fraud vectors. However, pooling transactional intelligence introduces severe regulatory and legal risks under:
- **The Kenya Data Protection Act (DPA 2019)** and Office of the Data Protection Commissioner (ODPC) guidance on lawful processing and automated decision-making.
- **Central Bank of Kenya (CBK) Prudential Guidelines** on risk management, cybersecurity, and consumer protection.
- **The National Payment System Act** and statutory anti-money laundering (AML/CFT) frameworks.

This whitepaper defines the **Kinga Privacy and Security Architecture**, establishing how participating institutions share fraud signals with mathematical privacy guarantees, zero raw Personally Identifiable Information (PII) disclosure, and verifiable cryptographic integrity.

---

## 2. Threat Model & Security Objectives

Kinga is architected against five adversarial threat categories:

| Threat Category | Adversary Goal | Architectural Mitigation |
| :--- | :--- | :--- |
| **Curious or Compromised Member** | Probe the consortium to deanonymize competitor customers or compile intelligence dossiers. | Keyed HMAC-SHA256 tokenization with domain separation; k-anonymity privacy floor ($k \ge 2$). Queries for single-source reports return 404. |
| **Network Eavesdropper** | Intercept transit traffic to harvest subscriber MSISDNs, bank accounts, or device IDs. | Mutual TLS (mTLS) with pinned certificates; raw PII is tokenized inside the member's private enclave before transmission. |
| **Malicious Accuser / Bad Actor Member** | Submit false fraud allegations to disrupt competitor services or harass legitimate customers. | Cryptographic hash chaining in an append-only ledger; member non-repudiation; contributor dispute workflows; governance remediation. |
| **Data Scraping & Dictionary Attacks** | Precompute rainbow tables of all Kenyan mobile numbers (+254 7XX / 1XX) to invert tokens. | Keyed HMAC with a high-entropy 256-bit secret key; rotating keyrings; rate limits per authenticated tenant. |
| **Stale or Poisoned Indicators** | Perpetually blacklist accounts following transient compromise or remediation. | Exponential confidence decay (30-day half-life); hard 90-day TTL expiration; dispute API. |

---

## 3. Cryptographic Tokenization Architecture

### 3.1 Domain-Separated Keyed Hashing

Plain SHA-256 or unkeyed hashing is strictly forbidden in Kinga because the space of Kenyan telephone numbers and account identifiers is small enough to permit offline brute-force precomputation.

Kinga employs **HMAC-SHA256** hosted within an isolated, minimal Rust service (`services/tokenizer`). All hashing operations enforce explicit **domain separation**:

```
Network Scope Token:
Token = HMAC-SHA256(NetworkKey, "network:" || Namespace || ":" || Value)

Tenant Scope Token:
Token = HMAC-SHA256(NetworkKey, "tenant:" || TenantID || ":" || Namespace || ":" || Value)
```

- **Namespace Restrictions:** Tokens are strictly partitioned by entity class (`msisdn`, `account`, `device`, `beneficiary`, `agent`, `wallet`).
- **Scope Partitioning:** Tenant-scoped tokens ensure an institution can correlate internal entities across events without linkability to other members' tenant-scoped tokens. Network-scoped tokens are used exclusively for cross-institution indicator matching.
- **Zero-Storage Enclave:** The tokenizer microservice operates entirely statelessly in memory. It has no disk persistence, database access, or outbound network connectivity, returning strict `Cache-Control: no-store` headers.

### 3.2 Key Ceremony & Rotation

The Network Key is generated during a formal **Key Ceremony** utilizing dual-control threshold custody. Key rotation is supported without downtime:
1. The Rust keyring supports an `active_version` alongside authorized legacy versions.
2. Token requests can optionally request prior key versions to facilitate planned migrations.
3. Key rotations are coordinated under agreed consortium migration windows.

---

## 4. Consortium Ledger & The Privacy Floor

### 4.1 Tamper-Evident Hash-Chained Ledger

The consortium indicator store maintains a verifiable audit trail of every member action. Each record contains:
- `Sequence`: Monotonically increasing 64-bit integer.
- `Action`: One of `contribute`, `dispute`, `remove`, `reinstate`.
- `Tenant`: Authenticated member submitting the transaction.
- `Indicator`: The network token, confidence score, and expiration timestamp.
- `PreviousHash`: Hex-encoded SHA-256 digest of the immediately preceding record.
- `Hash`: Hex-encoded SHA-256 digest of the current record.

This structure guarantees that records cannot be altered, inserted, or truncated without immediately invalidating all subsequent cryptographic links.

### 4.2 The Deterministic Privacy Floor ($k \ge 2$)

To protect consumers against unilateral false positives and prevent members from fishing for competitor intelligence:
1. When Institution A reports an indicator, the record is committed to the ledger but remains **unobservable** to other network members.
2. A lookup request (`GET /v1/consortium/indicators/{namespace}/{token}`) evaluates the number of distinct, authenticated member institutions that have contributed the token.
3. If `Confirmations < 2`, the API returns `404 indicator_not_found_or_below_privacy_threshold`.
4. Only when at least **two independent member institutions** confirm the risk does the consortium expose the indicator.
5. In query responses, contributor identities and tenant IDs are completely redacted. The querying institution receives only the aggregated confidence score and number of independent confirmations.

### 4.3 Exponential Confidence Decay

Fraud indicators are inherently perishable. Stolen SIM cards are recovered, compromised accounts are secured, and device fingerprints change ownership. 

Kinga models confidence decay continuously using a **30-day half-life**:

$$C(t) = \frac{1}{N} \sum_{i=1}^N C_{0,i} \times 2^{-\frac{t - t_{0,i}}{30 \times 24\text{h}}}$$

If no subsequent institution reconfirms the indicator, its contribution to the fraud score decays smoothly toward zero. Upon reaching the maximum TTL (bounded to 90 days), the indicator is permanently purged from active evaluation.

---

## 5. Multi-Tenant Entity Graph Privacy

The Kinga Entity Graph (`tools/graph/build_graph.py` and `services/gateway/internal/graph`) identifies syndicates and mule rings by synthesizing topological graph properties across institution boundaries:
- **Node Degree:** Total distinct connections across rails.
- **Inbound/Outbound Fan Ratios:** Rapid concentration and dispersion of funds.
- **Independent Tenant Bridges:** Number of distinct institutions connected through the entity.
- **Community Clustering:** Graph community detection algorithms identify dense subgraphs representing collusive syndicates.

### Graph Privacy Enforcements:
- Raw transaction amounts, customer names, and branch codes are excluded from graph inputs; only normalized, tokenized identifiers and relative edge weights are ingested.
- Graph lookup in the online gateway strictly enforces the `ConsortiumMinConfirmations` threshold: features for entities observed by only one tenant are suppressed.
- Graph snapshots are distributed as immutable, version-tagged artifacts (`kinga.graph.v1`) and loaded atomically into memory.

---

## 6. Regulatory Compliance Mapping: Kenya DPA 2019

| Principle (Section 3 of DPA 2019) | Kinga Technical & Operational Implementation |
| :--- | :--- |
| **Lawfulness, Fairness & Transparency** | Processing is rooted in legitimate fraud prevention interests (AML/CFT). Indicators reflect empirical multi-institutional confirmation rather than opaque automated profiling. |
| **Purpose Limitation** | Tokens and consortium queries are strictly restricted to fraud prevention and risk scoring. Secondary commercial use is prohibited by API design and charter. |
| **Data Minimization** | Zero plaintext PII is stored or transmitted in the consortium layer. Only 64-character hex tokens with domain separation are handled. |
| **Accuracy** | Exponential half-life decay, bounded 90-day TTLs, and instant contributor dispute APIs ensure stale or erroneous indicators are eliminated. |
| **Storage Limitation** | Ephemeral memory tokenization, mandatory expiration dates, and retention bounds prevent indefinite storage of consumer data. |
| **Integrity & Confidentiality** | HMAC-SHA256, mTLS, signed HMAC-SHA256 API request signatures with replay timestamps, and hash-chained audit ledgers. |
| **Automated Decision-Making Safeguards** | Consortium hits in Kinga produce `HOLD` or `STEP_UP` actions for human review or out-of-band verification, never immediate irreversible termination without recourse. |

---

## 7. Governance, Dispute Handling, and Member Rights

1. **Member Onboarding:** Every participating institution undergoes identity verification, signs the Consortium Charter and Data Sharing Agreement, and establishes mutual incident contact escalation trees.
2. **Dispute Recourse:** If an institution determines that an indicator was generated due to false suspicion (e.g. customer identity restored after theft), the contributing member calls `POST /v1/consortium/indicators/{namespace}/{token}/disputes`. The indicator is immediately suspended from matching.
3. **Governance Dual Control:** Reinstatement or permanent removal of disputed indicators is restricted to designated consortium governance administrators with mandatory recorded justification.
4. **Auditability:** Members may inspect their own contribution statistics and verify cryptographic chain continuity at any time.

---

*Authored by the Kinga Architecture & Privacy Working Group.*
