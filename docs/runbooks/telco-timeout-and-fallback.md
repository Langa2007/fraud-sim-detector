# Runbook: Telco Dependency Timeout and Degraded Fallback

## 1. Symptoms & Alerts
- Client responses contain `"degraded": true`.
- In `GET /metrics`, the ratio of `kinga_assessment_degraded_total` to `kinga_assessments_total` spikes above normal baseline (< 1%).
- End-to-end assess latency approaches or exceeds 100 ms.

## 2. Root Cause Analysis
1. Telco connector latency exceeded the timeout budget (60 ms).
2. The connector (Simulator fixture or CAMARA MNO API) returned a 404, 500, or timed out.
3. The number queried is not present in the telco lookup registry or provider cache.

## 3. Immediate Actions
1. Verify if the upstream telco endpoint / simulator file is reachable:
   - For simulator: verify `KINGA_TELCO_SIMULATOR_PATH` exists, is readable, and contains valid JSON.
   - For live CAMARA API: check telco provider status page or test endpoint with `curl` directly.
2. Check gateway logs for `telco lookup error` or `context deadline exceeded`.
3. In Phase 1 shadow mode, Kinga intentionally fails-open to local feature evaluation (device first-seen, beneficiary first-seen, balance drain) and continues scoring, flagging `degraded: true` in the evidence log.

## 4. Remediation
- If using simulator: restart gateway with updated simulator JSON fixture containing the active test MSISDNs.
- If upstream MNO is down: notify client operations. No manual intervention on core decisioning is required since shadow mode never blocks production transactions.
