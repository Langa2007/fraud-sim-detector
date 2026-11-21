use serde::{Deserialize, Serialize};
use std::{
    env, fs,
    io::{Read, Write},
    net::{TcpListener, TcpStream},
    path::Path,
    sync::Arc,
    sync::Mutex,
    thread,
    time::Instant,
};
use tract_onnx::prelude::*;

const FEATURE_ORDER: [&str; 15] = [
    "amount_kes",
    "sim_swap_age_minutes",
    "sim_swap_known",
    "device_first_seen",
    "beneficiary_first_seen",
    "balance_drain_ratio",
    "transfers_in_window",
    "new_beneficiaries_in_window",
    "window_minutes",
    "account_age_days",
    "account_age_known",
    "inbound_fan_in",
    "rapid_outbound",
    "overnight",
    "is_funds_transfer",
];

type OnnxPlan = SimplePlan<TypedFact, Box<dyn TypedOp>, Graph<TypedFact, Box<dyn TypedOp>>>;

#[derive(Deserialize)]
struct ModelCard {
    model_version: String,
    feature_schema_version: String,
    feature_order: Vec<String>,
    calibration: Calibration,
    operating_threshold: f64,
}

#[derive(Deserialize)]
struct Calibration {
    method: String,
    slope: f64,
    intercept: f64,
}

struct ModelRuntime {
    card: ModelCard,
    plan: Mutex<OnnxPlan>,
}

impl ModelRuntime {
    fn load(model_path: &str, card_path: &str) -> Result<Self, String> {
        let card_json = fs::read_to_string(card_path)
            .map_err(|e| format!("cannot read model card {card_path}: {e}"))?;
        let card: ModelCard =
            serde_json::from_str(&card_json).map_err(|e| format!("invalid model card: {e}"))?;
        let expected: Vec<String> = FEATURE_ORDER.iter().map(|v| (*v).to_string()).collect();
        if card.model_version.is_empty()
            || card.feature_schema_version != "kinga.features.v1"
            || card.feature_order != expected
            || card.calibration.method != "platt_scaling"
            || !card.calibration.slope.is_finite()
            || !card.calibration.intercept.is_finite()
            || !card.operating_threshold.is_finite()
            || !(0.0..=1.0).contains(&card.operating_threshold)
        {
            return Err(
                "model card schema, feature order, calibration or threshold is invalid".into(),
            );
        }
        let plan = tract_onnx::onnx()
            .model_for_path(Path::new(model_path))
            .map_err(|e| format!("cannot load ONNX model: {e}"))?
            .into_optimized()
            .map_err(|e| format!("cannot optimize ONNX model: {e}"))?
            .into_runnable()
            .map_err(|e| format!("cannot prepare ONNX model: {e}"))?;
        let probe = tract_ndarray::Array2::<f32>::zeros((1, FEATURE_ORDER.len()));
        let outputs = plan
            .run(tvec!(probe.into_tensor().into()))
            .map_err(|e| format!("ONNX startup inference failed: {e}"))?;
        let has_binary_probabilities = outputs.iter().any(|value| {
            value
                .to_array_view::<f32>()
                .map(|array| array.len() == 2)
                .unwrap_or(false)
        });
        if !has_binary_probabilities {
            return Err("ONNX model must expose two float class probabilities".into());
        }
        Ok(Self {
            card,
            plan: Mutex::new(plan),
        })
    }

    fn score(&self, event: &Event) -> Result<f64, String> {
        let values = event.features();
        let input = tract_ndarray::Array2::from_shape_vec((1, FEATURE_ORDER.len()), values)
            .map_err(|e| format!("cannot create model input: {e}"))?;
        let plan = self
            .plan
            .lock()
            .map_err(|_| "model runtime lock poisoned".to_string())?;
        let outputs = plan
            .run(tvec!(input.into_tensor().into()))
            .map_err(|e| format!("ONNX inference failed: {e}"))?;
        let raw = outputs
            .iter()
            .find_map(|value| {
                let output = value.to_array_view::<f32>().ok()?;
                (output.len() == 2)
                    .then(|| output.iter().copied().nth(1))
                    .flatten()
            })
            .ok_or_else(|| "ONNX model returned no binary probability output".to_string())?
            as f64;
        if !raw.is_finite() || !(0.0..=1.0).contains(&raw) {
            return Err("ONNX model returned an invalid probability".into());
        }
        let raw = raw.clamp(1e-6, 1.0 - 1e-6);
        let logit = (raw / (1.0 - raw)).ln();
        let calibrated = 1.0
            / (1.0
                + (-(self.card.calibration.slope * logit + self.card.calibration.intercept)).exp());
        if !calibrated.is_finite() {
            return Err("model returned a non-finite score".into());
        }
        Ok(calibrated.clamp(0.0, 1.0))
    }
}

#[derive(Clone, Deserialize)]
struct Policy {
    policy_version: String,
    mode: String,
    #[serde(default)]
    score_bands: Bands,
    rules: Vec<Rule>,
}
#[derive(Clone, Deserialize)]
struct Bands {
    #[serde(default = "medium_default")]
    medium: f64,
    #[serde(default = "high_default")]
    high: f64,
    #[serde(default = "critical_default")]
    critical: f64,
}
impl Default for Bands {
    fn default() -> Self {
        Self {
            medium: 0.2,
            high: 0.35,
            critical: 0.65,
        }
    }
}
fn medium_default() -> f64 {
    0.2
}
fn high_default() -> f64 {
    0.35
}
fn critical_default() -> f64 {
    0.65
}
#[derive(Clone, Deserialize, Default)]
struct Rule {
    id: String,
    reason: String,
    description: String,
    action: String,
    #[serde(default)]
    weight: f64,
    #[serde(default)]
    threshold_kes: f64,
    #[serde(default)]
    sim_swap_age_minutes_lt: i64,
    #[serde(default)]
    transfers_gte: i64,
    #[serde(default)]
    new_beneficiaries_gte: i64,
    #[serde(default)]
    window_minutes_lte: i64,
    #[serde(default)]
    drain_ratio_gte: f64,
    #[serde(default)]
    account_age_days_lt: i64,
    #[serde(default)]
    inbound_fan_in_gte: i64,
    #[serde(default)]
    mule_likelihood_gte: f64,
}
#[derive(Deserialize)]
struct Request {
    #[serde(rename = "tenant")]
    _tenant: String,
    event: Event,
}
#[derive(Deserialize)]
struct Event {
    tenant_event_id: String,
    occurred_at: String,
    channel: String,
    #[serde(default)]
    event_type: String,
    #[serde(default)]
    transaction: Transaction,
    #[serde(default)]
    signals: Signals,
}
#[derive(Default, Deserialize)]
struct Transaction {
    #[serde(default)]
    amount: f64,
}
#[derive(Default, Deserialize)]
#[allow(dead_code)]
struct Signals {
    sim_swap_age_minutes: Option<i64>,
    telco_available: Option<bool>,
    #[serde(default)]
    device_first_seen: bool,
    #[serde(default)]
    beneficiary_first_seen: bool,
    balance_drain_ratio: Option<f64>,
    #[serde(default)]
    transfers_in_window: i64,
    #[serde(default)]
    new_beneficiaries_in_window: i64,
    #[serde(default)]
    window_minutes: i64,
    account_age_days: Option<i64>,
    #[serde(default)]
    inbound_fan_in: i64,
    #[serde(default)]
    rapid_outbound: bool,
    #[serde(default)]
    beneficiary_consortium_hit: bool,
    #[serde(default)]
    consortium_confirmations: i64,
    consortium_confidence: Option<f64>,
    graph_mule_likelihood: Option<f64>,
    #[serde(default)]
    agent_float_anomaly: bool,
}

impl Event {
    fn features(&self) -> Vec<f32> {
        let s = &self.signals;
        let hour = self
            .occurred_at
            .split('T')
            .nth(1)
            .and_then(|v| v.get(0..2))
            .and_then(|v| v.parse::<u32>().ok())
            .unwrap_or(12);
        vec![
            self.transaction.amount.min(f32::MAX as f64) as f32,
            s.sim_swap_age_minutes.unwrap_or(0) as f32,
            s.sim_swap_age_minutes.is_some() as u8 as f32,
            s.device_first_seen as u8 as f32,
            s.beneficiary_first_seen as u8 as f32,
            s.balance_drain_ratio.unwrap_or(0.0) as f32,
            s.transfers_in_window as f32,
            s.new_beneficiaries_in_window as f32,
            s.window_minutes as f32,
            s.account_age_days.unwrap_or(0) as f32,
            s.account_age_days.is_some() as u8 as f32,
            s.inbound_fan_in as f32,
            s.rapid_outbound as u8 as f32,
            (!(5..22).contains(&hour)) as u8 as f32,
            (self.event_type == "funds_transfer") as u8 as f32,
        ]
    }
}
#[derive(Serialize)]
struct Reason {
    code: String,
    detail: String,
    weight: f64,
}
#[derive(Serialize)]
struct Response {
    decision: String,
    mode: String,
    risk_score: f64,
    risk_band: String,
    reasons: Vec<Reason>,
    recommended_actions: Vec<String>,
    degraded: bool,
    tenant_event_id: String,
    policy_version: String,
    model_version: Option<String>,
    latency_ms: u128,
    evidence_id: String,
}

fn evaluate(policy: &Policy, request: Request, model: Option<&ModelRuntime>) -> Response {
    let start = Instant::now();
    let ev = request.event;
    let telco_degraded = ev.signals.telco_available == Some(false);
    let s = &ev.signals;
    let mut reasons: Vec<Reason> = Vec::new();
    let mut minimum_decision = "ALLOW";
    for rule in &policy.rules {
        let matched = match rule.id.as_str() {
            "swap_new_device_new_beneficiary" => {
                s.sim_swap_age_minutes
                    .map(|age| age >= 0 && age < rule.sim_swap_age_minutes_lt)
                    .unwrap_or(false)
                    && s.device_first_seen
                    && s.beneficiary_first_seen
                    && ev.transaction.amount >= rule.threshold_kes
            }
            "beneficiary_velocity" => {
                s.transfers_in_window >= rule.transfers_gte
                    && s.new_beneficiaries_in_window >= rule.new_beneficiaries_gte
                    && s.window_minutes > 0
                    && s.window_minutes <= rule.window_minutes_lte
            }
            "balance_drain" => s
                .balance_drain_ratio
                .map(|ratio| ratio >= rule.drain_ratio_gte)
                .unwrap_or(false),
            "young_account_fan_in_out" => {
                s.account_age_days
                    .map(|age| age >= 0 && age < rule.account_age_days_lt)
                    .unwrap_or(false)
                    && s.inbound_fan_in >= rule.inbound_fan_in_gte
                    && s.rapid_outbound
            }
            "beneficiary_consortium_hit" => s.beneficiary_consortium_hit,
            "mule_graph_anomaly" => s
                .graph_mule_likelihood
                .map(|val| {
                    let threshold = if rule.mule_likelihood_gte > 0.0 {
                        rule.mule_likelihood_gte
                    } else {
                        0.70
                    };
                    val >= threshold
                })
                .unwrap_or(false),
            "agent_float_movement_anomaly" => s.agent_float_anomaly,
            _ => false,
        };
        if matched {
            if decision_rank(&rule.action) > decision_rank(minimum_decision) {
                minimum_decision = &rule.action;
            }
            reasons.push(Reason {
                code: rule.reason.clone(),
                detail: rule.description.clone(),
                weight: rule.weight,
            });
        }
    }
    let hour = ev
        .occurred_at
        .split('T')
        .nth(1)
        .and_then(|v| v.get(0..2))
        .and_then(|v| v.parse::<u32>().ok())
        .unwrap_or(12);
    if !reasons.is_empty()
        && !(5..22).contains(&hour)
        && let Some(rule) = policy.rules.iter().find(|r| r.id == "overnight_activity")
    {
        if decision_rank(&rule.action) > decision_rank(minimum_decision) {
            minimum_decision = &rule.action;
        }
        reasons.push(Reason {
            code: rule.reason.clone(),
            detail: format!("{} Channel: {}.", rule.description, ev.channel),
            weight: rule.weight,
        });
    }
    reasons.sort_by(|a, b| {
        b.weight
            .total_cmp(&a.weight)
            .then_with(|| a.code.cmp(&b.code))
    });
    let rule_score: f64 = reasons
        .iter()
        .map(|r| r.weight)
        .sum::<f64>()
        .clamp(0.0, 1.0);
    let model_score = model.and_then(|m| m.score(&ev).ok());
    if let (Some(runtime), Some(probability)) = (model, model_score)
        && probability >= runtime.card.operating_threshold
    {
        reasons.push(Reason {
            code: "MODEL_HIGH_RISK".into(),
            detail: format!(
                "Calibrated model probability {:.1}% reached the tenant operating threshold.",
                probability * 100.0
            ),
            weight: probability,
        });
    }
    reasons.sort_by(|a, b| {
        b.weight
            .total_cmp(&a.weight)
            .then_with(|| a.code.cmp(&b.code))
    });
    let score = rule_score.max(model_score.unwrap_or(0.0));
    let (band, score_decision) = if score >= policy.score_bands.critical {
        ("critical", "HOLD")
    } else if score >= policy.score_bands.high {
        ("high", "STEP_UP")
    } else if score >= policy.score_bands.medium {
        ("medium", "STEP_UP")
    } else {
        ("low", "ALLOW")
    };
    let decision = if decision_rank(minimum_decision) > decision_rank(score_decision) {
        minimum_decision
    } else {
        score_decision
    };
    let mut actions = Vec::new();
    if decision != "ALLOW" {
        actions.push("ALERT_FRAUD_TEAM".into())
    }
    if decision == "STEP_UP" {
        actions.push("RECOMMEND_STEP_UP".into())
    }
    if decision == "HOLD" {
        actions.push("RECOMMEND_HOLD".into())
    }
    Response {
        decision: decision.into(),
        mode: "shadow".into(),
        risk_score: score,
        risk_band: band.into(),
        reasons,
        recommended_actions: actions,
        degraded: telco_degraded || model.is_some() && model_score.is_none(),
        tenant_event_id: ev.tenant_event_id,
        policy_version: policy.policy_version.clone(),
        model_version: model
            .filter(|_| model_score.is_some())
            .map(|runtime| runtime.card.model_version.clone()),
        latency_ms: start.elapsed().as_millis(),
        evidence_id: String::new(),
    }
}

fn decision_rank(action: &str) -> u8 {
    match action {
        "STEP_UP" => 1,
        "HOLD" => 2,
        _ => 0,
    }
}

fn read_request(stream: &mut TcpStream) -> std::io::Result<(String, Vec<u8>)> {
    let mut header = Vec::new();
    let mut byte = [0u8; 1];
    while header.len() < 16_384 {
        stream.read_exact(&mut byte)?;
        header.push(byte[0]);
        if header.ends_with(b"\r\n\r\n") {
            break;
        }
    }
    let text = String::from_utf8_lossy(&header);
    let first = text.lines().next().unwrap_or("").to_string();
    let mut length = 0usize;
    for line in text.lines().skip(1) {
        if let Some((k, v)) = line.split_once(':')
            && k.eq_ignore_ascii_case("content-length")
        {
            length = v.trim().parse().unwrap_or(0)
        }
    }
    if length > 1_048_576 {
        return Ok((first, Vec::new()));
    }
    let mut body = vec![0; length];
    stream.read_exact(&mut body)?;
    Ok((first, body))
}
fn respond(stream: &mut TcpStream, status: &str, body: &[u8]) {
    let header = format!(
        "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    let _ = stream.write_all(header.as_bytes());
    let _ = stream.write_all(body);
}
fn handle(mut stream: TcpStream, policy: Arc<Policy>, model: Arc<Option<ModelRuntime>>) {
    let _ = stream.set_read_timeout(Some(std::time::Duration::from_millis(200)));
    let _ = stream.set_write_timeout(Some(std::time::Duration::from_millis(200)));
    let result = read_request(&mut stream);
    let (line, body) = match result {
        Ok(v) => v,
        Err(_) => {
            respond(
                &mut stream,
                "400 Bad Request",
                b"{\"error\":\"bad_request\"}",
            );
            return;
        }
    };
    if line.starts_with("GET /healthz ") {
        respond(&mut stream, "200 OK", b"{\"status\":\"ok\"}");
        return;
    }
    if !line.starts_with("POST /internal/v1/assess ") {
        respond(&mut stream, "404 Not Found", b"{\"error\":\"not_found\"}");
        return;
    }
    match serde_json::from_slice::<Request>(&body) {
        Ok(req) => {
            let response = evaluate(&policy, req, model.as_ref().as_ref());
            match serde_json::to_vec(&response) {
                Ok(bytes) => respond(&mut stream, "200 OK", &bytes),
                Err(_) => respond(
                    &mut stream,
                    "500 Internal Server Error",
                    b"{\"error\":\"serialize\"}",
                ),
            }
        }
        Err(_) => respond(
            &mut stream,
            "400 Bad Request",
            b"{\"error\":\"invalid_engine_request\"}",
        ),
    }
}
fn main() {
    let policy_path =
        env::var("KINGA_POLICY_PATH").unwrap_or_else(|_| "policies/default.json".into());
    let raw = fs::read_to_string(&policy_path)
        .unwrap_or_else(|e| panic!("cannot read policy {policy_path}: {e}"));
    let policy: Policy = serde_json::from_str(&raw).expect("invalid policy configuration");
    if policy.mode != "shadow"
        || policy.rules.is_empty()
        || !(0.0 < policy.score_bands.medium
            && policy.score_bands.medium < policy.score_bands.high
            && policy.score_bands.high < policy.score_bands.critical
            && policy.score_bands.critical <= 1.0)
    {
        panic!("invalid policy mode, rule list, or score bands")
    };
    let mut seen = std::collections::HashSet::new();
    for r in &policy.rules {
        if !seen.insert(r.id.as_str())
            || r.weight < 0.0
            || r.weight > 1.0
            || r.description.is_empty()
            || !["ALLOW", "STEP_UP", "HOLD"].contains(&r.action.as_str())
        {
            panic!("duplicate rule or invalid weight: {}", r.id)
        }
        if ![
            "swap_new_device_new_beneficiary",
            "beneficiary_velocity",
            "balance_drain",
            "young_account_fan_in_out",
            "overnight_activity",
        ]
        .contains(&r.id.as_str())
        {
            panic!("unsupported rule id: {}", r.id)
        }
    }
    let model_path = env::var("KINGA_MODEL_PATH").unwrap_or_default();
    let card_env = env::var("KINGA_MODEL_CARD_PATH").unwrap_or_default();
    let model = if !model_path.trim().is_empty() {
        let card_path = if card_env.trim().is_empty() {
            Path::new(&model_path)
                .with_file_name("model.json")
                .to_string_lossy()
                .to_string()
        } else {
            card_env
        };
        Some(
            ModelRuntime::load(&model_path, &card_path)
                .unwrap_or_else(|e| panic!("model startup failed: {e}")),
        )
    } else {
        if !card_env.trim().is_empty() {
            panic!("KINGA_MODEL_CARD_PATH requires KINGA_MODEL_PATH")
        }
        None
    };
    if let Some(runtime) = &model {
        eprintln!("loaded fraud model {}", runtime.card.model_version);
    }
    let addr = env::var("KINGA_ENGINE_ADDR").unwrap_or_else(|_| "0.0.0.0:8090".into());
    let listener = TcpListener::bind(&addr).expect("cannot bind decision engine");
    eprintln!(
        "Rust decision engine listening on {addr} with policy {}",
        policy.policy_version
    );
    let policy = Arc::new(policy);
    let model = Arc::new(model);
    for connection in listener.incoming() {
        match connection {
            Ok(stream) => {
                let p = Arc::clone(&policy);
                let m = Arc::clone(&model);
                thread::spawn(move || handle(stream, p, m));
            }
            Err(e) => eprintln!("accept error: {e}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_policy() -> Policy {
        Policy {
            policy_version: "test-2026-1.0".into(),
            mode: "shadow".into(),
            score_bands: Bands {
                medium: 0.2,
                high: 0.35,
                critical: 0.65,
            },
            rules: vec![
                Rule {
                    id: "swap_new_device_new_beneficiary".into(),
                    reason: "SIM_SWAP_RECENT".into(),
                    description: "Recent SIM swap with new device and beneficiary".into(),
                    action: "STEP_UP".into(),
                    weight: 0.4,
                    threshold_kes: 50_000.0,
                    sim_swap_age_minutes_lt: 1440,
                    transfers_gte: 0,
                    new_beneficiaries_gte: 0,
                    window_minutes_lte: 0,
                    drain_ratio_gte: 0.0,
                    account_age_days_lt: 0,
                    inbound_fan_in_gte: 0,
                    ..Default::default()
                },
                Rule {
                    id: "balance_drain".into(),
                    reason: "BALANCE_DRAIN_PATTERN".into(),
                    description: "Large balance drain ratio".into(),
                    action: "HOLD".into(),
                    weight: 0.3,
                    threshold_kes: 0.0,
                    sim_swap_age_minutes_lt: 0,
                    transfers_gte: 0,
                    new_beneficiaries_gte: 0,
                    window_minutes_lte: 0,
                    drain_ratio_gte: 0.7,
                    account_age_days_lt: 0,
                    inbound_fan_in_gte: 0,
                    ..Default::default()
                },
                Rule {
                    id: "overnight_activity".into(),
                    reason: "OVERNIGHT_ACTIVITY".into(),
                    description: "Activity during off-hours".into(),
                    action: "STEP_UP".into(),
                    weight: 0.15,
                    threshold_kes: 0.0,
                    sim_swap_age_minutes_lt: 0,
                    transfers_gte: 0,
                    new_beneficiaries_gte: 0,
                    window_minutes_lte: 0,
                    drain_ratio_gte: 0.0,
                    account_age_days_lt: 0,
                    inbound_fan_in_gte: 0,
                    ..Default::default()
                },
                Rule {
                    id: "beneficiary_consortium_hit".into(),
                    reason: "BENEFICIARY_CONSORTIUM_HIT".into(),
                    description: "Beneficiary flagged by consortium members".into(),
                    action: "HOLD".into(),
                    weight: 0.45,
                    ..Default::default()
                },
                Rule {
                    id: "mule_graph_anomaly".into(),
                    reason: "MULE_GRAPH_RISK".into(),
                    description: "High mule likelihood in network graph".into(),
                    action: "HOLD".into(),
                    weight: 0.35,
                    mule_likelihood_gte: 0.70,
                    ..Default::default()
                },
                Rule {
                    id: "agent_float_movement_anomaly".into(),
                    reason: "AGENT_FLOAT_ANOMALY".into(),
                    description: "Rapid float depletion or structured smurfing".into(),
                    action: "HOLD".into(),
                    weight: 0.30,
                    ..Default::default()
                },
            ],
        }
    }

    #[test]
    fn test_allow_clean_transaction() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_clean_1".into(),
                occurred_at: "2026-10-31T14:00:00+03:00".into(),
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 500.0 },
                signals: Signals {
                    sim_swap_age_minutes: None,
                    telco_available: Some(true),
                    device_first_seen: false,
                    beneficiary_first_seen: false,
                    balance_drain_ratio: Some(0.1),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "ALLOW");
        assert_eq!(res.risk_band, "low");
        assert_eq!(res.risk_score, 0.0);
        assert_eq!(res.mode, "shadow");
        assert!(!res.degraded);
        assert!(res.reasons.is_empty());
    }

    #[test]
    fn test_sim_swap_triggers_step_up() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_swap_1".into(),
                occurred_at: "2026-10-31T14:00:00+03:00".into(),
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 60_000.0 },
                signals: Signals {
                    sim_swap_age_minutes: Some(38),
                    telco_available: Some(true),
                    device_first_seen: true,
                    beneficiary_first_seen: true,
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "STEP_UP");
        assert_eq!(res.risk_band, "high");
        assert_eq!(res.risk_score, 0.4);
        assert_eq!(res.reasons.len(), 1);
        assert_eq!(res.reasons[0].code, "SIM_SWAP_RECENT");
    }

    #[test]
    fn test_sim_swap_plus_drain_critical_hold() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_critical_1".into(),
                occurred_at: "2026-10-31T23:30:00+03:00".into(), // overnight
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 100_000.0 },
                signals: Signals {
                    sim_swap_age_minutes: Some(15),
                    telco_available: Some(true),
                    device_first_seen: true,
                    beneficiary_first_seen: true,
                    balance_drain_ratio: Some(0.85),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "HOLD");
        assert_eq!(res.risk_band, "critical");
        assert!(res.risk_score >= 0.7);
        // Should match SIM swap, balance drain, and overnight activity
        assert_eq!(res.reasons.len(), 3);
        assert!(
            res.recommended_actions
                .contains(&"ALERT_FRAUD_TEAM".to_string())
        );
        assert!(
            res.recommended_actions
                .contains(&"RECOMMEND_HOLD".to_string())
        );
    }

    #[test]
    fn test_degraded_mode_when_telco_unavailable() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_degraded_1".into(),
                occurred_at: "2026-10-31T12:00:00+03:00".into(),
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 1_000.0 },
                signals: Signals {
                    sim_swap_age_minutes: None,
                    telco_available: Some(false),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert!(res.degraded);
        assert_eq!(res.decision, "ALLOW");
    }

    #[test]
    fn test_beneficiary_consortium_hit_triggers_hold() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_consortium_1".into(),
                occurred_at: "2026-10-31T12:00:00+03:00".into(),
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 15_000.0 },
                signals: Signals {
                    beneficiary_consortium_hit: true,
                    consortium_confirmations: 3,
                    consortium_confidence: Some(0.88),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "HOLD");
        assert_eq!(res.risk_band, "high");
        assert_eq!(res.risk_score, 0.45);
        assert_eq!(res.reasons.len(), 1);
        assert_eq!(res.reasons[0].code, "BENEFICIARY_CONSORTIUM_HIT");
    }

    #[test]
    fn test_mule_graph_anomaly_triggers_hold() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_graph_1".into(),
                occurred_at: "2026-10-31T12:00:00+03:00".into(),
                channel: "mobile_app".into(),
                event_type: "funds_transfer".into(),
                transaction: Transaction { amount: 20_000.0 },
                signals: Signals {
                    graph_mule_likelihood: Some(0.85),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "HOLD");
        assert_eq!(res.risk_band, "high");
        assert_eq!(res.risk_score, 0.35);
        assert_eq!(res.reasons.len(), 1);
        assert_eq!(res.reasons[0].code, "MULE_GRAPH_RISK");
    }

    #[test]
    fn test_agent_float_anomaly_triggers_hold() {
        let policy = test_policy();
        let req = Request {
            _tenant: "demo".into(),
            event: Event {
                tenant_event_id: "evt_mule_1".into(),
                occurred_at: "2026-10-31T12:00:00+03:00".into(),
                channel: "agent".into(),
                event_type: "agent_float_movement".into(),
                transaction: Transaction { amount: 98_000.0 },
                signals: Signals {
                    agent_float_anomaly: true,
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req, None);
        assert_eq!(res.decision, "HOLD");
        assert_eq!(res.risk_score, 0.30);
        assert_eq!(res.reasons.len(), 1);
        assert_eq!(res.reasons[0].code, "AGENT_FLOAT_ANOMALY");
    }
}
