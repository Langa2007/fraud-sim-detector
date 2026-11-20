const mockDecisions = [
  {
    tenant_event_id: "evt_01J9ZK3Q",
    tenant: "demo",
    occurred_at: "2026-10-31T22:14:09+03:00",
    event_type: "funds_transfer",
    channel: "mobile_app",
    amount: 250000,
    currency: "KES",
    msisdn: "+254712345678",
    decision: "HOLD",
    mode: "shadow",
    risk_score: 0.93,
    risk_band: "critical",
    reasons: [
      { code: "SIM_SWAP_RECENT", detail: "SIM replaced 38 minutes ago", weight: 0.41 },
      { code: "BALANCE_DRAIN_PATTERN", detail: "3 transfers, 85% of balance in 12 min", weight: 0.24 },
      { code: "NEW_DEVICE", detail: "First time seen on tenant account", weight: 0.13 },
      { code: "OVERNIGHT_ACTIVITY", detail: "Activity outside 05:00-22:00 window", weight: 0.15 },
    ],
    recommended_actions: ["ALERT_FRAUD_TEAM", "RECOMMEND_HOLD"],
    degraded: false,
    evidence_id: "ev_1_f8a912b3c4d5e6f7",
    latency_ms: 24,
  },
  {
    tenant_event_id: "evt_01J9ZK4A",
    tenant: "demo",
    occurred_at: "2026-10-31T22:20:00+03:00",
    event_type: "funds_transfer",
    channel: "mobile_app",
    amount: 65000,
    currency: "KES",
    msisdn: "+254722111222",
    decision: "STEP_UP",
    mode: "shadow",
    risk_score: 0.45,
    risk_band: "high",
    reasons: [
      { code: "SIM_SWAP_RECENT", detail: "SIM swapped within last 24h", weight: 0.4 },
    ],
    recommended_actions: ["ALERT_FRAUD_TEAM", "RECOMMEND_STEP_UP"],
    degraded: false,
    evidence_id: "ev_2_771bc45e2a39df88",
    latency_ms: 18,
  },
  {
    tenant_event_id: "evt_01J9ZK5B",
    tenant: "demo",
    occurred_at: "2026-10-31T14:10:15+03:00",
    event_type: "funds_transfer",
    channel: "mobile_app",
    amount: 1500,
    currency: "KES",
    msisdn: "+254700987654",
    decision: "ALLOW",
    mode: "shadow",
    risk_score: 0.05,
    risk_band: "low",
    reasons: [],
    recommended_actions: [],
    degraded: false,
    evidence_id: "ev_3_1029384756abcdef",
    latency_ms: 8,
  },
  {
    tenant_event_id: "evt_01J9ZK6C",
    tenant: "demo",
    occurred_at: "2026-10-31T16:45:00+03:00",
    event_type: "funds_transfer",
    channel: "ussd",
    amount: 5000,
    currency: "KES",
    msisdn: "+254799000111",
    decision: "ALLOW",
    mode: "shadow",
    risk_score: 0.0,
    risk_band: "low",
    reasons: [],
    recommended_actions: [],
    degraded: true,
    evidence_id: "ev_4_abcdef0123456789",
    latency_ms: 61,
  },
];

const mockPolicy = {
  policy_version: "2026-10-28.3",
  mode: "shadow",
  score_bands: {
    medium: 0.2,
    high: 0.35,
    critical: 0.65,
  },
  rules: [
    {
      id: "swap_new_device_new_beneficiary",
      reason: "SIM_SWAP_RECENT",
      description: "Recent SIM swap (< 24h) combined with first-seen device and first-seen beneficiary over threshold (50,000 KES)",
      action: "STEP_UP",
      weight: 0.4,
    },
    {
      id: "beneficiary_velocity",
      reason: "VELOCITY_THRESHOLD_EXCEEDED",
      description: "Multiple outbound transfers to new beneficiaries within tight velocity window",
      action: "HOLD",
      weight: 0.35,
    },
    {
      id: "balance_drain",
      reason: "BALANCE_DRAIN_PATTERN",
      description: "Balance drain exceeding 70% within short time window outside 90-day baseline",
      action: "HOLD",
      weight: 0.3,
    },
    {
      id: "young_account_fan_in_out",
      reason: "MULE_FAN_IN_OUT_SUSPICION",
      description: "New account (< 30 days) with high inbound fan-in followed by rapid outbound pass-through",
      action: "STEP_UP",
      weight: 0.25,
    },
    {
      id: "overnight_activity",
      reason: "OVERNIGHT_ACTIVITY",
      description: "High-risk activity occurring during off-hours (22:00 to 05:00 local time)",
      action: "STEP_UP",
      weight: 0.15,
    },
  ],
};

function renderExplorer(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Recent Shadow Decisions</h2>
      <p style="color: var(--text-muted); margin-bottom: 1rem; font-size: 0.85rem;">
        Inspecting real-time risk scores and tamper-evident evidence logs for tenant events.
      </p>
      <table>
        <thead>
          <tr>
            <th>Event ID</th>
            <th>Occurred At</th>
            <th>Channel</th>
            <th>MSISDN / Subject</th>
            <th>Amount</th>
            <th>Risk Score</th>
            <th>Decision (Shadow)</th>
            <th>Reasons</th>
            <th>Evidence ID</th>
            <th>Latency</th>
          </tr>
        </thead>
        <tbody>
          ${mockDecisions
            .map(
              (d) => `
            <tr>
              <td class="mono"><strong>${d.tenant_event_id}</strong></td>
              <td>${new Date(d.occurred_at).toLocaleTimeString()}</td>
              <td><span class="badge" style="background:#334155">${d.channel}</span></td>
              <td class="mono">${d.msisdn || "—"}</td>
              <td>${d.amount ? `${d.amount.toLocaleString()} ${d.currency}` : "—"}</td>
              <td>
                <span class="badge badge-${d.risk_band}">
                  ${(d.risk_score * 100).toFixed(0)}% (${d.risk_band})
                </span>
              </td>
              <td>
                <span class="badge badge-${d.decision.toLowerCase().replace("_", "-")}">
                  ${d.decision}
                </span>
                ${d.degraded ? '<span title="Telco signal unavailable" style="cursor:help; margin-left:4px;">⚠️</span>' : ""}
              </td>
              <td style="font-size:0.8rem;">
                ${
                  d.reasons.length > 0
                    ? d.reasons.map((r) => `<div>• <strong>${r.code}</strong> (${r.weight})</div>`).join("")
                    : '<span style="color:var(--text-muted)">Clean</span>'
                }
              </td>
              <td class="mono" style="font-size:0.8rem; color: var(--accent);">${d.evidence_id}</td>
              <td class="mono" style="font-size:0.85rem; color:${d.latency_ms > 100 ? "var(--warning)" : "var(--success)"};">
                ${d.latency_ms} ms
              </td>
            </tr>
          `
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderRules(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Active Policy: <span class="mono" style="color:var(--accent);">${mockPolicy.policy_version}</span></h2>
      <div style="margin-bottom: 1.5rem; display: flex; gap: 2rem;">
        <div><strong>Execution Mode:</strong> <span class="badge badge-allow">${mockPolicy.mode.toUpperCase()}</span></div>
        <div><strong>Medium Band:</strong> ≥ ${mockPolicy.score_bands.medium}</div>
        <div><strong>High Band:</strong> ≥ ${mockPolicy.score_bands.high}</div>
        <div><strong>Critical Band:</strong> ≥ ${mockPolicy.score_bands.critical}</div>
      </div>
      <table>
        <thead>
          <tr>
            <th>Rule ID</th>
            <th>Reason Code</th>
            <th>Description</th>
            <th>Recommended Action</th>
            <th>Weight</th>
          </tr>
        </thead>
        <tbody>
          ${mockPolicy.rules
            .map(
              (r) => `
            <tr>
              <td class="mono"><strong>${r.id}</strong></td>
              <td class="mono" style="color:var(--accent);">${r.reason}</td>
              <td>${r.description}</td>
              <td><span class="badge badge-${r.action.toLowerCase().replace("_", "-")}">${r.action}</span></td>
              <td class="mono"><strong>${r.weight}</strong></td>
            </tr>
          `
            )
            .join("")}
        </tbody>
      </table>
    </div>
  `;
}

function renderReport(container) {
  container.innerHTML = `
    <div class="stat-grid">
      <div class="stat-card">
        <div class="label">Total Replayed Events</div>
        <div class="value">10,000</div>
        <div class="target">Baseline Dataset</div>
      </div>
      <div class="stat-card">
        <div class="label">Confirmed Fraud Recall</div>
        <div class="value" style="color: #34d399;">94.2%</div>
        <div class="target">✓ Exceeds 90% Target</div>
      </div>
      <div class="stat-card">
        <div class="label">False Positive Rate (FPR)</div>
        <div class="value" style="color: #60a5fa;">0.8%</div>
        <div class="target">✓ Under 1.5% Ceiling</div>
      </div>
      <div class="stat-card">
        <div class="label">p99 Latency</div>
        <div class="value" style="color: #34d399;">38 ms</div>
        <div class="target">✓ Well below 150 ms Budget</div>
      </div>
    </div>

    <div class="card">
      <h2>Phase 1 Exit Gate Readiness</h2>
      <table>
        <thead>
          <tr>
            <th>Requirement</th>
            <th>Target</th>
            <th>Measured Value</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>p99 Assess Latency</strong></td>
            <td>≤ 150 ms</td>
            <td class="mono">38 ms</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Tamper-Evident Evidence Trail</strong></td>
            <td>100% of decisions verifiable</td>
            <td class="mono">100% (10,000 / 10,000)</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Deterministic Replay</strong></td>
            <td>Zero deviation on replay</td>
            <td class="mono">100% byte-identical</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Explainability</strong></td>
            <td>100% with reason codes</td>
            <td class="mono">100% carrying reason & weights</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
        </tbody>
      </table>
    </div>
  `;
}

document.addEventListener("DOMContentLoaded", () => {
  const app = document.getElementById("app");
  if (!app) return;

  const tabs = document.querySelectorAll(".tab-btn");

  function switchTab(target) {
    tabs.forEach((t) => {
      if (t.getAttribute("data-tab") === target) {
        t.classList.add("active");
      } else {
        t.classList.remove("active");
      }
    });

    if (target === "explorer") renderExplorer(app);
    else if (target === "rules") renderRules(app);
    else if (target === "report") renderReport(app);
  }

  tabs.forEach((btn) => {
    btn.addEventListener("click", () => {
      const tab = btn.getAttribute("data-tab");
      if (tab) switchTab(tab);
    });
  });

  renderExplorer(app);
});
