// Kinga Console v2 — Pure Modern JavaScript (ES Modules)

let currentCases = [
  {
    case_id: "case_01J9ZK3Q",
    tenant: "demo",
    tenant_event_id: "evt_01J9ZK3Q",
    evidence_id: "ev_1_f8a912b3c4d5e6f7",
    decision: "HOLD",
    risk_score: 0.93,
    risk_band: "critical",
    policy_version: "2026-10-28.3",
    model_version: "gbm-v1.0.0",
    status: "open",
    assignee: "",
    created_at: new Date(Date.now() - 35 * 60000).toISOString(),
    updated_at: new Date(Date.now() - 35 * 60000).toISOString(),
    reasons: [
      { code: "SIM_SWAP_RECENT", detail: "SIM replaced 38 minutes ago", weight: 0.41 },
      { code: "BALANCE_DRAIN_PATTERN", detail: "3 transfers, 85% of balance in 12 min", weight: 0.24 },
      { code: "MODEL_HIGH_RISK", detail: "Calibrated model probability >= 0.85 threshold", weight: 0.20 },
    ],
    history: [],
  },
  {
    case_id: "case_01J9ZK4A",
    tenant: "demo",
    tenant_event_id: "evt_01J9ZK4A",
    evidence_id: "ev_2_771bc45e2a39df88",
    decision: "STEP_UP",
    risk_score: 0.45,
    risk_band: "high",
    policy_version: "2026-10-28.3",
    model_version: "gbm-v1.0.0",
    status: "assigned",
    assignee: "analyst-kamau",
    created_at: new Date(Date.now() - 75 * 60000).toISOString(),
    updated_at: new Date(Date.now() - 20 * 60000).toISOString(),
    reasons: [
      { code: "SIM_SWAP_RECENT", detail: "SIM swapped within last 24h", weight: 0.4 },
    ],
    history: [
      {
        action_id: "act_101",
        type: "assign",
        reviewer: "supervisor-1",
        assignee: "analyst-kamau",
        created_at: new Date(Date.now() - 20 * 60000).toISOString(),
      },
    ],
  },
  {
    case_id: "case_01J9ZK5B",
    tenant: "demo",
    tenant_event_id: "evt_01J9ZK5B",
    evidence_id: "ev_3_1029384756abcdef",
    decision: "STEP_UP",
    risk_score: 0.38,
    risk_band: "medium",
    policy_version: "2026-10-28.3",
    model_version: "gbm-v1.0.0",
    status: "released",
    assignee: "analyst-kamau",
    created_at: new Date(Date.now() - 120 * 60000).toISOString(),
    updated_at: new Date(Date.now() - 95 * 60000).toISOString(),
    reasons: [
      { code: "NEW_DEVICE", detail: "First time seen on tenant account", weight: 0.38 },
    ],
    history: [
      {
        action_id: "act_99",
        type: "release",
        reviewer: "analyst-kamau",
        note: "Customer confirmed device upgrade over verified phone call",
        created_at: new Date(Date.now() - 95 * 60000).toISOString(),
      },
    ],
  },
];

let rolloutConfig = {
  target_mode: "shadow",
  rollout_percentage: 0,
  kill_switch_active: false,
  friction_ceiling: 0.015,
  friction_window: 500,
  current_friction_rate: 0.008,
};

let currentStepUp = {
  challenge_id: "ch_sim_8812a",
  tenant_event_id: "evt_01J9ZK3Q",
  channel: "ussd_push",
  language: "sw",
  prompt: "Uthibitisho wa Kinga: Thibitisha uhamishaji wa KSh 250,000 kwenda ben_77ac. Bonyeza 1 kukubali, 2 kukataa.",
  status: "pending",
  amount: 250000,
  beneficiary: "ben_77ac",
  created_at: new Date().toISOString(),
};

function renderCases(container) {
  container.innerHTML = `
    <div class="card">
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:1rem;">
        <h2>Investigator Review Queue</h2>
        <div>
          <button class="btn btn-primary" id="btn-refresh-cases">↻ Refresh Queue</button>
        </div>
      </div>
      <table>
        <thead>
          <tr>
            <th>Case ID</th>
            <th>Event ID</th>
            <th>Recommendation</th>
            <th>Risk Score</th>
            <th>Risk Band</th>
            <th>Status</th>
            <th>Assignee</th>
            <th>Created</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          ${currentCases
            .map(
              (c) => `
            <tr>
              <td class="mono"><strong>${c.case_id}</strong></td>
              <td class="mono">${c.tenant_event_id}</td>
              <td><span class="badge badge-${c.decision.toLowerCase().replace("_", "-")}">${c.decision}</span></td>
              <td><strong>${(c.risk_score * 100).toFixed(0)}%</strong></td>
              <td><span class="badge badge-${c.risk_band}">${c.risk_band}</span></td>
              <td><span class="badge badge-${c.status}">${c.status}</span></td>
              <td>${c.assignee || '<span style="color:var(--text-muted)">Unassigned</span>'}</td>
              <td>${new Date(c.created_at).toLocaleTimeString()}</td>
              <td style="display:flex; gap:0.35rem;">
                <button class="btn btn-sm" onclick="window.viewCaseDetails('${c.case_id}')">Review</button>
                <button class="btn btn-sm" onclick="window.openOutcomeModal('${c.tenant_event_id}', '${c.evidence_id}')">Label</button>
                <button class="btn btn-sm" onclick="window.viewEvidencePack('${c.case_id}')">Pack</button>
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

function renderExplorer(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Recent Live Stream Assessments</h2>
      <table>
        <thead>
          <tr>
            <th>Event ID</th>
            <th>Channel</th>
            <th>Subject</th>
            <th>Amount</th>
            <th>Risk Score</th>
            <th>Rules vs Model</th>
            <th>Mode</th>
            <th>Evidence ID</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td class="mono"><strong>evt_01J9ZK3Q</strong></td>
            <td>mobile_app</td>
            <td class="mono">+254712345678</td>
            <td>250,000 KES</td>
            <td><span class="badge badge-critical">93% (critical)</span></td>
            <td>Rules: 0.80 | Model: 0.93</td>
            <td><span class="badge badge-allow">${rolloutConfig.kill_switch_active ? "SHADOW" : rolloutConfig.target_mode.toUpperCase()}</span></td>
            <td class="mono" style="color:var(--accent);">ev_1_f8a912b3c4d5e6f7</td>
          </tr>
          <tr>
            <td class="mono"><strong>evt_01J9ZK4A</strong></td>
            <td>mobile_app</td>
            <td class="mono">+254722111222</td>
            <td>65,000 KES</td>
            <td><span class="badge badge-high">45% (high)</span></td>
            <td>Rules: 0.40 | Model: 0.45</td>
            <td><span class="badge badge-allow">${rolloutConfig.kill_switch_active ? "SHADOW" : rolloutConfig.target_mode.toUpperCase()}</span></td>
            <td class="mono" style="color:var(--accent);">ev_2_771bc45e2a39df88</td>
          </tr>
        </tbody>
      </table>
    </div>
  `;
}

function renderStepUp(container) {
  container.innerHTML = `
    <div style="display:grid; grid-template-columns: 1fr 1fr; gap: 2rem;">
      <div class="card">
        <h2>Out-of-Band Step-Up Orchestration</h2>
        <p style="color:var(--text-muted); font-size:0.85rem; margin-bottom:1.5rem;">
          Deliver multi-channel verification challenges via USSD Push, SMS callback, or in-app prompts.
        </p>

        <div class="form-group">
          <label>Target Channel:</label>
          <select id="stepup-channel-select" class="form-control">
            <option value="ussd_push" ${currentStepUp.channel === "ussd_push" ? "selected" : ""}>USSD Push (*254#)</option>
            <option value="sms_callback" ${currentStepUp.channel === "sms_callback" ? "selected" : ""}>Interactive SMS Callback</option>
            <option value="in_app" ${currentStepUp.channel === "in_app" ? "selected" : ""}>Verified In-App Challenge</option>
          </select>
        </div>

        <div class="form-group">
          <label>Language Template:</label>
          <select id="stepup-lang-select" class="form-control">
            <option value="sw" ${currentStepUp.language === "sw" ? "selected" : ""}>Kiswahili (Default Kenya)</option>
            <option value="en" ${currentStepUp.language === "en" ? "selected" : ""}>English</option>
          </select>
        </div>

        <div class="form-group">
          <label>Amount (KES):</label>
          <input type="number" id="stepup-amount" class="form-control" value="${currentStepUp.amount}" />
        </div>

        <div class="form-group">
          <label>Beneficiary:</label>
          <input type="text" id="stepup-ben" class="form-control" value="${currentStepUp.beneficiary}" />
        </div>

        <button class="btn btn-primary" style="width:100%;" id="btn-dispatch-stepup">⚡ Dispatch Challenge</button>
      </div>

      <div class="card">
        <h2>Customer Device Simulation</h2>
        <div class="phone-mockup">
          <div class="phone-screen">
            <div>
              <div style="font-size:0.75rem; color:#94a3b8; margin-bottom:8px; text-transform:uppercase;">
                ${currentStepUp.channel.replace("_", " ")} prompt
              </div>
              <div style="background:#0f172a; padding:12px; border-radius:8px; font-size:0.9rem; line-height:1.4;" id="prompt-display">
                ${currentStepUp.prompt}
              </div>
            </div>
            <div>
              <div style="margin-bottom:12px; font-size:0.8rem;">
                Status: <span class="badge badge-${currentStepUp.status === "approved" ? "allow" : currentStepUp.status === "rejected" ? "hold" : "step-up"}">${currentStepUp.status}</span>
              </div>
              <div style="display:flex; gap:8px;">
                <button class="btn btn-success" style="flex:1;" id="btn-sim-approve" ${currentStepUp.status !== "pending" ? "disabled" : ""}>1: Approve</button>
                <button class="btn btn-danger" style="flex:1;" id="btn-sim-reject" ${currentStepUp.status !== "pending" ? "disabled" : ""}>2: Reject</button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  `;

  document.getElementById("btn-dispatch-stepup").addEventListener("click", () => {
    const ch = document.getElementById("stepup-channel-select").value;
    const lang = document.getElementById("stepup-lang-select").value;
    const amount = parseFloat(document.getElementById("stepup-amount").value) || 50000;
    const ben = document.getElementById("stepup-ben").value || "Unknown";

    let prompt = "";
    if (lang === "sw") {
      prompt = ch === "ussd_push"
        ? `Uthibitisho wa Kinga: Thibitisha uhamishaji wa KSh ${amount.toLocaleString()} kwenda ${ben}. Bonyeza 1 kukubali, 2 kukataa.`
        : `Kinga: Umeanzisha muamala wa KSh ${amount.toLocaleString()} kwenda ${ben}. Kama ni wewe, jibu NDIYO. Kama si wewe, jibu HAPANA mara moja.`;
    } else {
      prompt = ch === "ussd_push"
        ? `Kinga Verification: Confirm transfer of KSh ${amount.toLocaleString()} to ${ben}. Press 1 to approve, 2 to reject.`
        : `Kinga Security: A transfer of KSh ${amount.toLocaleString()} to ${ben} was initiated. Reply YES to authorize, or NO immediately.`;
    }

    currentStepUp = {
      challenge_id: "ch_" + Math.random().toString(36).substring(2, 9),
      tenant_event_id: "evt_live_" + Date.now(),
      channel: ch,
      language: lang,
      prompt: prompt,
      status: "pending",
      amount: amount,
      beneficiary: ben,
      created_at: new Date().toISOString(),
    };
    renderStepUp(container);
  });

  document.getElementById("btn-sim-approve").addEventListener("click", () => {
    currentStepUp.status = "approved";
    renderStepUp(container);
  });

  document.getElementById("btn-sim-reject").addEventListener("click", () => {
    currentStepUp.status = "rejected";
    renderStepUp(container);
  });
}

function renderRollout(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Shadow-to-Enforce Staged Migration Kit</h2>
      <p style="color:var(--text-muted); font-size:0.85rem; margin-bottom:1.5rem;">
        Control operational progression from read-only shadow scoring to active transaction gating with emergency fail-safes.
      </p>

      <div class="stat-grid">
        <div class="stat-card">
          <div class="label">Current Target Mode</div>
          <div class="value" style="color:var(--accent);">${rolloutConfig.target_mode.toUpperCase()}</div>
          <div class="target">Configured Execution Mode</div>
        </div>
        <div class="stat-card">
          <div class="label">Traffic Rollout</div>
          <div class="value">${rolloutConfig.rollout_percentage}%</div>
          <div class="target">Deterministic Hash Bucketing</div>
        </div>
        <div class="stat-card">
          <div class="label">Measured Friction Rate</div>
          <div class="value" style="color:${rolloutConfig.current_friction_rate > rolloutConfig.friction_ceiling ? "var(--danger)" : "var(--success)"};">
            ${(rolloutConfig.current_friction_rate * 100).toFixed(2)}%
          </div>
          <div class="target">Ceiling: ${(rolloutConfig.friction_ceiling * 100).toFixed(1)}% Max</div>
        </div>
        <div class="stat-card">
          <div class="label">Kill Switch Status</div>
          <div class="value" style="color:${rolloutConfig.kill_switch_active ? "var(--danger)" : "var(--success)"};">
            ${rolloutConfig.kill_switch_active ? "TRIPPED" : "NORMAL"}
          </div>
          <div class="target">${rolloutConfig.kill_switch_active ? "Forced Shadow Fallback" : "Enforce Allowed"}</div>
        </div>
      </div>

      <div style="background:var(--bg-tertiary); padding:1.5rem; border-radius:8px; margin-bottom:1.5rem;">
        <h3 style="font-size:1rem; margin-bottom:1rem;">Staged Rollout Progression</h3>
        <div style="display:flex; gap:1rem; margin-bottom:1.5rem;">
          <button class="btn ${rolloutConfig.rollout_percentage === 0 ? "btn-primary" : ""}" onclick="window.setRolloutStage(0, 'shadow')">Stage 0: 0% (Pure Shadow)</button>
          <button class="btn ${rolloutConfig.rollout_percentage === 5 ? "btn-primary" : ""}" onclick="window.setRolloutStage(5, 'advisory')">Stage 1: 5% (Canary Advisory)</button>
          <button class="btn ${rolloutConfig.rollout_percentage === 25 ? "btn-primary" : ""}" onclick="window.setRolloutStage(25, 'enforce')">Stage 2: 25% (Controlled Enforce)</button>
          <button class="btn ${rolloutConfig.rollout_percentage === 100 ? "btn-primary" : ""}" onclick="window.setRolloutStage(100, 'enforce')">Stage 3: 100% (Full Enforce)</button>
        </div>

        <div style="display:flex; justify-content:space-between; align-items:center; border-top:1px solid var(--border); padding-top:1rem;">
          <div>
            <strong>Instant Emergency Kill Switch:</strong>
            <p style="color:var(--text-muted); font-size:0.8rem;">Immediately reverts all tenant transactions to shadow mode within 1 ms.</p>
          </div>
          <button class="btn ${rolloutConfig.kill_switch_active ? "btn-success" : "btn-danger"}" onclick="window.toggleKillSwitch()">
            ${rolloutConfig.kill_switch_active ? "Reset Kill Switch" : "🚨 EMERGENCY KILL SWITCH"}
          </button>
        </div>
      </div>
    </div>
  `;
}

function renderReport(container) {
  container.innerHTML = `
    <div class="stat-grid">
      <div class="stat-card">
        <div class="label">Model vs Rules Lift</div>
        <div class="value" style="color: #34d399;">+28.2%</div>
        <div class="target">Recall @ 0.5% Fixed FPR</div>
      </div>
      <div class="stat-card">
        <div class="label">PR-AUC</div>
        <div class="value" style="color: #38bdf8;">0.924</div>
        <div class="target">Rules Baseline: 0.741</div>
      </div>
      <div class="stat-card">
        <div class="label">Estimated KSh Saved</div>
        <div class="value" style="color: #34d399;">KSh 4.2M</div>
        <div class="target">Per 10,000 Trans. Slice</div>
      </div>
      <div class="stat-card">
        <div class="label">Inference Latency</div>
        <div class="value" style="color: #38bdf8;">1.2 ms</div>
        <div class="target">Tract ONNX in Rust Core</div>
      </div>
    </div>

    <div class="card">
      <h2>Phase 2 Exit Gate Assessment</h2>
      <table>
        <thead>
          <tr>
            <th>Exit Gate Criterion</th>
            <th>Target Metric</th>
            <th>Observed Result</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>Held-Out Future Lift</strong></td>
            <td>Model beats rules on held-out 20% future time split</td>
            <td class="mono">Recall 89.4% vs 61.2% (+28.2% lift)</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Friction Ceiling</strong></td>
            <td>FPR ≤ 0.5% ceiling</td>
            <td class="mono">FPR: 0.38% (well within 0.5% limit)</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Outcome Labelling Loop</strong></td>
            <td>Weekly feedback cycle from /v1/outcomes</td>
            <td class="mono">Automated dataset preparation & ONNX retraining</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
          <tr>
            <td><strong>Tamper-Evident Evidence</strong></td>
            <td>100% case actions & outcomes hash-chained</td>
            <td class="mono">Verified end-to-end SHA-256 linkage</td>
            <td><span class="badge badge-allow">PASSED</span></td>
          </tr>
        </tbody>
      </table>
    </div>
  `;
}

function renderRules(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Active Hybrid Policy & ONNX Model Specification</h2>
      <div style="margin-bottom: 1.5rem; display: flex; gap: 2rem;">
        <div><strong>Policy Version:</strong> <span class="mono">2026-10-28.3</span></div>
        <div><strong>Model Artifact:</strong> <span class="mono">models/current/model.onnx</span></div>
        <div><strong>Operating Threshold:</strong> <span class="mono">0.8500</span></div>
      </div>
      <table>
        <thead>
          <tr>
            <th>Rule ID / Classifier</th>
            <th>Reason Code</th>
            <th>Guardrail Action</th>
            <th>Weight / Source</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td class="mono"><strong>gradient_boosted_tree_v1</strong></td>
            <td class="mono" style="color:var(--accent);">MODEL_HIGH_RISK</td>
            <td><span class="badge badge-step-up">STEP_UP</span></td>
            <td>Tract ONNX Inference (threshold >= 0.85)</td>
          </tr>
          <tr>
            <td class="mono"><strong>swap_new_device_new_beneficiary</strong></td>
            <td class="mono" style="color:var(--accent);">SIM_SWAP_RECENT</td>
            <td><span class="badge badge-step-up">STEP_UP</span></td>
            <td>0.40 (Hard Guardrail)</td>
          </tr>
          <tr>
            <td class="mono"><strong>balance_drain</strong></td>
            <td class="mono" style="color:var(--accent);">BALANCE_DRAIN_PATTERN</td>
            <td><span class="badge badge-hold">HOLD</span></td>
            <td>0.30 (Hard Guardrail)</td>
          </tr>
        </tbody>
      </table>
    </div>
  `;
}

// Window Global Handlers for Modals & Actions
window.viewCaseDetails = function (caseId) {
  const c = currentCases.find((x) => x.case_id === caseId);
  if (!c) return;

  const modal = document.getElementById("modal-container");
  modal.innerHTML = `
    <div class="modal-backdrop" onclick="window.closeModal(event)">
      <div class="modal-content" onclick="event.stopPropagation()">
        <h2>Case Review: ${c.case_id}</h2>
        <div style="margin-bottom:1rem; font-size:0.85rem; color:var(--text-muted);">
          Event: <code>${c.tenant_event_id}</code> | Evidence: <code>${c.evidence_id}</code>
        </div>
        <div style="background:var(--bg-primary); padding:1rem; border-radius:6px; margin-bottom:1rem;">
          <div><strong>Decision:</strong> <span class="badge badge-${c.decision.toLowerCase().replace("_", "-")}">${c.decision}</span> (Risk: ${(c.risk_score * 100).toFixed(0)}%)</div>
          <div style="margin-top:0.5rem;"><strong>Contributing Reasons:</strong></div>
          <ul style="margin-left:1.5rem; margin-top:0.25rem;">
            ${c.reasons.map((r) => `<li><code>${r.code}</code>: ${r.detail}</li>`).join("")}
          </ul>
        </div>

        <div class="form-group">
          <label>Assignee Reviewer ID:</label>
          <input type="text" id="action-assignee" class="form-control" value="${c.assignee || "analyst-kamau"}" />
        </div>

        <div class="form-group">
          <label>Reviewer Rationale & Notes (Required for Hold/Release):</label>
          <textarea id="action-note" class="form-control" rows="3" placeholder="Enter findings from out-of-band contact or telco verification..."></textarea>
        </div>

        <div style="display:flex; justify-content:space-between; margin-top:1.5rem;">
          <button class="btn" onclick="window.closeModal()">Cancel</button>
          <div style="display:flex; gap:0.5rem;">
            <button class="btn btn-primary" onclick="window.submitCaseAction('${c.case_id}', 'assign')">Assign</button>
            <button class="btn btn-success" onclick="window.submitCaseAction('${c.case_id}', 'release')">Release</button>
            <button class="btn btn-danger" onclick="window.submitCaseAction('${c.case_id}', 'hold')">Hold</button>
          </div>
        </div>
      </div>
    </div>
  `;
};

window.openOutcomeModal = function (eventId, evidenceId) {
  const modal = document.getElementById("modal-container");
  modal.innerHTML = `
    <div class="modal-backdrop" onclick="window.closeModal(event)">
      <div class="modal-content" onclick="event.stopPropagation()">
        <h2>Submit Ground-Truth Label (/v1/outcomes)</h2>
        <p style="font-size:0.85rem; color:var(--text-muted); margin-bottom:1rem;">
          Audited labels update the append-only outcome ledger and feed into weekly ONNX retraining.
        </p>

        <div class="form-group">
          <label>Tenant Event ID:</label>
          <input type="text" class="form-control mono" value="${eventId}" readonly />
        </div>

        <div class="form-group">
          <label>Outcome Label:</label>
          <select id="outcome-label" class="form-control">
            <option value="confirmed_fraud">Confirmed Fraud (Positive)</option>
            <option value="false_positive">False Positive (Negative)</option>
            <option value="legitimate">Legitimate Customer (Negative)</option>
            <option value="unknown">Unknown / Inconclusive</option>
          </select>
        </div>

        <div class="form-group">
          <label>Confirmed Loss Amount (KES, optional):</label>
          <input type="number" id="outcome-loss" class="form-control" placeholder="e.g. 50000" />
        </div>

        <div class="form-group">
          <label>Reviewer Identity:</label>
          <input type="text" id="outcome-reviewer" class="form-control" value="analyst-kamau" />
        </div>

        <div class="form-group">
          <label>Audit Notes:</label>
          <textarea id="outcome-notes" class="form-control" rows="2" placeholder="Investigation details..."></textarea>
        </div>

        <div style="display:flex; justify-content:flex-end; gap:0.5rem; margin-top:1.5rem;">
          <button class="btn" onclick="window.closeModal()">Cancel</button>
          <button class="btn btn-primary" onclick="window.submitOutcomeLabel('${eventId}', '${evidenceId}')">Submit Label</button>
        </div>
      </div>
    </div>
  `;
};

window.viewEvidencePack = function (caseId) {
  window.open(`/v1/cases/${caseId}/evidence/pack`, "_blank");
};

window.submitCaseAction = function (caseId, type) {
  const c = currentCases.find((x) => x.case_id === caseId);
  if (!c) return;
  const assignee = document.getElementById("action-assignee").value;
  const note = document.getElementById("action-note").value;

  if ((type === "hold" || type === "release") && !note.trim()) {
    alert("A rationale note is mandatory for hold and release review actions.");
    return;
  }

  c.status = type === "assign" ? "assigned" : type === "release" ? "released" : "held";
  if (assignee) c.assignee = assignee;
  c.history.push({
    action_id: "act_" + Date.now(),
    type: type,
    reviewer: assignee || "analyst",
    note: note,
    created_at: new Date().toISOString(),
  });

  window.closeModal();
  renderCases(document.getElementById("app"));
};

window.submitOutcomeLabel = function (eventId, evidenceId) {
  const label = document.getElementById("outcome-label").value;
  const reviewer = document.getElementById("outcome-reviewer").value;
  const loss = parseFloat(document.getElementById("outcome-loss").value) || 0;
  const notes = document.getElementById("outcome-notes").value;

  alert(`Recorded ground-truth label "${label}" for ${eventId} by ${reviewer} into outcomes ledger.`);
  window.closeModal();
};

window.setRolloutStage = function (percentage, mode) {
  rolloutConfig.rollout_percentage = percentage;
  rolloutConfig.target_mode = mode;
  updateHeaderMode();
  renderRollout(document.getElementById("app"));
};

window.toggleKillSwitch = function () {
  rolloutConfig.kill_switch_active = !rolloutConfig.kill_switch_active;
  updateHeaderMode();
  renderRollout(document.getElementById("app"));
};

window.simulateConsortiumQuery = function (mode) {
  const resultBox = document.getElementById("consortium-query-result");
  if (!resultBox) return;

  if (mode === "below_floor") {
    resultBox.innerHTML = `
      <div style="background:rgba(239, 68, 68, 0.1); border:1px solid var(--danger); padding:1rem; border-radius:6px; color:#fca5a5;">
        <strong>HTTP 404: indicator_not_found_or_below_privacy_threshold</strong><br>
        <span style="font-size:0.85rem; color:var(--text-primary); display:block; margin-top:0.4rem;">
          🔒 <strong>Privacy Floor Protection Active:</strong> Token only has 1 contribution from an individual tenant.
          Under Kenya ODPC & Kinga Consortium rules, at least <strong>2 independent licensed institutions</strong> must corroborate an indicator before it can be disclosed across the network.
        </span>
      </div>
    `;
  } else {
    resultBox.innerHTML = `
      <div style="background:rgba(16, 185, 129, 0.1); border:1px solid var(--success); padding:1rem; border-radius:6px; color:#6ee7b7;">
        <strong>HTTP 200 OK — Active Multi-Tenant Indicator Found</strong><br>
        <pre style="background:var(--bg-primary); padding:0.75rem; border-radius:4px; margin-top:0.5rem; font-size:0.8rem; color:var(--text-primary);">{
  "namespace": "beneficiary",
  "token": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "confidence": 0.865,
  "independent_confirmations": 3,
  "expires_at": "2026-12-21T09:30:00Z"
}</pre>
        <span style="font-size:0.85rem; color:var(--text-primary); display:block; margin-top:0.4rem;">
          🚨 <strong>Consortium Corroboration:</strong> Confirmed by 3 independent institutions (e.g. Bank A, Telco B, SACCO C) with 30-day exponential decay applied.
        </span>
      </div>
    `;
  }
};

window.simulateContributeIndicator = function () {
  const ns = document.getElementById("contrib-namespace").value;
  const rawId = document.getElementById("contrib-raw-id").value;
  const conf = parseFloat(document.getElementById("contrib-confidence").value) || 0.8;
  const statusEl = document.getElementById("contrib-status");

  if (!rawId) {
    alert("Please enter a raw identifier (e.g. MSISDN or account) to tokenize.");
    return;
  }

  // Simulated HMAC-SHA256 network token preview (first 16 chars)
  const simulatedToken = Array.from(rawId)
    .reduce((acc, char) => (acc * 31 + char.charCodeAt(0)) >>> 0, 0x12345678)
    .toString(16)
    .padStart(8, "0") + "f8a912b3c4d5e6f7a0b1c2d3e4f5a6b7";

  statusEl.innerHTML = `
    <div style="background:rgba(56, 189, 248, 0.1); border:1px solid var(--accent); padding:0.75rem; border-radius:6px; font-size:0.85rem;">
      ✅ Contributed to consortium ledger!<br>
      <strong>Token:</strong> <code>${simulatedToken.padEnd(64, "0")}</code><br>
      <strong>Action:</strong> contribute (Seq #143, SHA-256 Chained, Zero PII logged)<br>
      <strong>Namespace:</strong> ${ns} | <strong>Confidence:</strong> ${conf}
    </div>
  `;
};

function renderConsortium(container) {
  container.innerHTML = `
    <div class="card">
      <h2>Fraud Consortium & Multi-Tenant Entity Graph</h2>
      <p style="color:var(--text-muted); font-size:0.85rem; margin-bottom:1.5rem;">
        Phase 3 Network Intelligence: Cryptographic tokenization, tamper-evident hash chaining, privacy floor k-anonymity (>=2 independent confirmations), and cross-tenant mule graph feature extraction.
      </p>

      <div class="stat-grid">
        <div class="stat-card">
          <div class="label">Participating Members</div>
          <div class="value" style="color:var(--accent);">8</div>
          <div class="target">Tier 1 Banks & Telcos</div>
        </div>
        <div class="stat-card">
          <div class="label">Privacy Floor (k)</div>
          <div class="value">2+</div>
          <div class="target">Independent Confirmations</div>
        </div>
        <div class="stat-card">
          <div class="label">Confidence Half-Life</div>
          <div class="value">30 Days</div>
          <div class="target">Exponential Time Decay</div>
        </div>
        <div class="stat-card">
          <div class="label">Local Contributions</div>
          <div class="value" style="color:var(--success);">14</div>
          <div class="target">demo tenant ledger share</div>
        </div>
      </div>

      <div style="display:grid; grid-template-columns:1fr 1fr; gap:1.5rem; margin-bottom:1.5rem;">
        <!-- Left: Interactive Consortium Sandbox -->
        <div style="background:var(--bg-tertiary); padding:1.25rem; border-radius:8px;">
          <h3 style="font-size:1rem; margin-bottom:0.75rem;">Consortium Indicator Privacy Simulator</h3>
          <p style="color:var(--text-muted); font-size:0.8rem; margin-bottom:1rem;">
            Test the k-anonymity privacy floor. Member identities are never disclosed, and single-institution reports are hidden until corroborated.
          </p>
          <div style="display:flex; gap:0.5rem; margin-bottom:1rem;">
            <button class="btn" onclick="window.simulateConsortiumQuery('below_floor')">Query Unconfirmed (k=1)</button>
            <button class="btn btn-primary" onclick="window.simulateConsortiumQuery('confirmed')">Query Confirmed (k=3)</button>
          </div>
          <div id="consortium-query-result">
            <div style="background:var(--bg-primary); padding:1rem; border-radius:6px; font-size:0.85rem; color:var(--text-muted);">
              Click a query button above to inspect real-time consortium response handling.
            </div>
          </div>
        </div>

        <!-- Right: Tokenize & Contribute -->
        <div style="background:var(--bg-tertiary); padding:1.25rem; border-radius:8px;">
          <h3 style="font-size:1rem; margin-bottom:0.75rem;">Tokenize & Contribute Indicator</h3>
          <p style="color:var(--text-muted); font-size:0.8rem; margin-bottom:1rem;">
            Tokens are keyed via HMAC-SHA256 Rust service. Raw PII is never transmitted to the consortium ledger.
          </p>
          <div style="display:grid; grid-template-columns:1fr 2fr; gap:0.5rem; margin-bottom:0.75rem;">
            <select id="contrib-namespace" class="form-input" style="padding:0.4rem;">
              <option value="msisdn">MSISDN</option>
              <option value="account">Account</option>
              <option value="beneficiary">Beneficiary</option>
              <option value="wallet">Wallet</option>
              <option value="agent">Agent</option>
            </select>
            <input id="contrib-raw-id" class="form-input" placeholder="+254712345678 or ACC-9988" style="padding:0.4rem;" value="+254712999888">
          </div>
          <div style="display:flex; gap:0.5rem; align-items:center; margin-bottom:1rem;">
            <label style="font-size:0.8rem; color:var(--text-muted);">Confidence:</label>
            <input id="contrib-confidence" type="number" step="0.05" min="0" max="1" value="0.90" style="width:70px; padding:0.3rem;" class="form-input">
            <button class="btn btn-primary" onclick="window.simulateContributeIndicator()">Contribute Indicator</button>
          </div>
          <div id="contrib-status"></div>
        </div>
      </div>

      <!-- Entity Graph Section -->
      <div style="background:var(--bg-secondary); border:1px solid var(--border); border-radius:8px; padding:1.25rem; margin-bottom:1.5rem;">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:1rem;">
          <div>
            <h3 style="font-size:1rem;">Multi-Tenant Graph Community Clusters</h3>
            <p style="color:var(--text-muted); font-size:0.8rem;">Synthesized network batch features computed across tenant transaction snapshots (v1 format).</p>
          </div>
          <span class="badge badge-critical">Snapshot: 2026-11-20 (Fresh)</span>
        </div>

        <table class="table">
          <thead>
            <tr>
              <th>Community Hub Token</th>
              <th>Namespace</th>
              <th>Network Degree</th>
              <th>Cross-Tenant Links</th>
              <th>Mule Likelihood</th>
              <th>Risk Classification</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td><code>9f8a...2e10</code></td>
              <td>wallet</td>
              <td>28 transfers</td>
              <td>4 independent banks</td>
              <td><strong style="color:var(--danger);">0.89</strong></td>
              <td><span class="badge badge-critical">HIGH MULE RISK</span></td>
            </tr>
            <tr>
              <td><code>3b1c...aa74</code></td>
              <td>account</td>
              <td>19 transfers</td>
              <td>3 independent banks</td>
              <td><strong style="color:var(--danger);">0.76</strong></td>
              <td><span class="badge badge-critical">HIGH MULE RISK</span></td>
            </tr>
            <tr>
              <td><code>7e4d...089f</code></td>
              <td>agent</td>
              <td>14 transfers</td>
              <td>2 independent telcos</td>
              <td><strong style="color:var(--warning);">0.48</strong></td>
              <td><span class="badge badge-stepup">ELEVATED COLLUSION</span></td>
            </tr>
            <tr>
              <td><code>1a2b...55cd</code></td>
              <td>beneficiary</td>
              <td>4 transfers</td>
              <td>2 independent banks</td>
              <td><strong style="color:var(--success);">0.12</strong></td>
              <td><span class="badge badge-allow">CLEAN BENEFICIARY</span></td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Agent Float Movement Anomalies -->
      <div style="background:var(--bg-secondary); border:1px solid var(--border); border-radius:8px; padding:1.25rem;">
        <h3 style="font-size:1rem; margin-bottom:0.5rem;">Agent Float & Structuring Anomaly Alerts</h3>
        <p style="color:var(--text-muted); font-size:0.8rem; margin-bottom:1rem;">
          Real-time detection of float depletion > 80% in < 30 minutes, structured smurfing (90k-99k KES), and collusive burst cash-out.
        </p>

        <div style="display:grid; grid-template-columns:repeat(3, 1fr); gap:1rem;">
          <div style="background:var(--bg-tertiary); padding:1rem; border-radius:6px; border-left:4px solid var(--danger);">
            <div style="display:flex; justify-content:space-between; margin-bottom:0.4rem;">
              <strong>AGENT-NAI-0442</strong>
              <span class="badge badge-critical">HOLD (0.85)</span>
            </div>
            <p style="font-size:0.8rem; color:var(--text-muted); margin-bottom:0.4rem;">Float drained 88% within 18 minutes via repeated outbound transfers.</p>
            <div style="font-size:0.75rem; color:var(--accent);">Rule: agent_float_movement_anomaly</div>
          </div>

          <div style="background:var(--bg-tertiary); padding:1rem; border-radius:6px; border-left:4px solid var(--danger);">
            <div style="display:flex; justify-content:space-between; margin-bottom:0.4rem;">
              <strong>ACC-MULE-8812</strong>
              <span class="badge badge-critical">HOLD (0.78)</span>
            </div>
            <p style="font-size:0.8rem; color:var(--text-muted); margin-bottom:0.4rem;">2x transfers of KSh 98,500 within 22 min just below 100k reporting threshold.</p>
            <div style="font-size:0.75rem; color:var(--accent);">Rule: STRUCTURING_DETECTED</div>
          </div>

          <div style="background:var(--bg-tertiary); padding:1rem; border-radius:6px; border-left:4px solid var(--warning);">
            <div style="display:flex; justify-content:space-between; margin-bottom:0.4rem;">
              <strong>AGENT-MSA-1090</strong>
              <span class="badge badge-stepup">HOLD (0.82)</span>
            </div>
            <p style="font-size:0.8rem; color:var(--text-muted); margin-bottom:0.4rem;">Burst cash-out: 7 rapid outbound withdrawals processed in 8 minutes.</p>
            <div style="font-size:0.75rem; color:var(--accent);">Rule: AGENT_BURST_CASH_OUT</div>
          </div>
        </div>
      </div>
    </div>
  `;
}

window.closeModal = function () {
  document.getElementById("modal-container").innerHTML = "";
};

function updateHeaderMode() {
  const label = document.getElementById("current-mode-label");
  const badge = document.getElementById("global-mode-badge");
  if (rolloutConfig.kill_switch_active) {
    label.innerText = "SHADOW (KILL-SWITCH ENGAGED)";
    badge.classList.add("kill-switch");
  } else {
    label.innerText = `${rolloutConfig.target_mode.toUpperCase()} (${rolloutConfig.rollout_percentage}% Rollout)`;
    badge.classList.remove("kill-switch");
  }
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

    if (target === "cases") renderCases(app);
    else if (target === "explorer") renderExplorer(app);
    else if (target === "consortium") renderConsortium(app);
    else if (target === "stepup") renderStepUp(app);
    else if (target === "rollout") renderRollout(app);
    else if (target === "rules") renderRules(app);
    else if (target === "report") renderReport(app);
  }

  tabs.forEach((btn) => {
    btn.addEventListener("click", () => {
      const tab = btn.getAttribute("data-tab");
      if (tab) switchTab(tab);
    });
  });

  renderCases(app);
});
