use serde::{Deserialize, Serialize};
use std::{
    env, fs,
    io::{Read, Write},
    net::{TcpListener, TcpStream},
    sync::Arc,
    thread,
    time::Instant,
};

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
#[derive(Clone, Deserialize)]
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

fn evaluate(policy: &Policy, request: Request) -> Response {
    let start = Instant::now();
    let ev = request.event;
    let telco_degraded = ev.signals.telco_available == Some(false);
    let s = ev.signals;
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
    let score: f64 = reasons
        .iter()
        .map(|r| r.weight)
        .sum::<f64>()
        .clamp(0.0, 1.0);
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
        degraded: telco_degraded,
        tenant_event_id: ev.tenant_event_id,
        policy_version: policy.policy_version.clone(),
        model_version: None,
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
fn handle(mut stream: TcpStream, policy: Arc<Policy>) {
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
            let response = evaluate(&policy, req);
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
    let addr = env::var("KINGA_ENGINE_ADDR").unwrap_or_else(|_| "0.0.0.0:8090".into());
    let listener = TcpListener::bind(&addr).expect("cannot bind decision engine");
    eprintln!(
        "Rust decision engine listening on {addr} with policy {}",
        policy.policy_version
    );
    let policy = Arc::new(policy);
    for connection in listener.incoming() {
        match connection {
            Ok(stream) => {
                let p = Arc::clone(&policy);
                thread::spawn(move || handle(stream, p));
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

        let res = evaluate(&policy, req);
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

        let res = evaluate(&policy, req);
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

        let res = evaluate(&policy, req);
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
                transaction: Transaction { amount: 1_000.0 },
                signals: Signals {
                    sim_swap_age_minutes: None,
                    telco_available: Some(false),
                    ..Default::default()
                },
            },
        };

        let res = evaluate(&policy, req);
        assert!(res.degraded);
        assert_eq!(res.decision, "ALLOW");
    }
}
