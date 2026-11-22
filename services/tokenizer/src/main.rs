use hmac::{Hmac, Mac};
use serde::{Deserialize, Serialize};
use sha2::Sha256;
use std::{
    env,
    io::{Read, Write},
    net::{TcpListener, TcpStream},
    sync::Arc,
    thread,
};

type HmacSha256 = Hmac<Sha256>;

#[derive(Clone, Deserialize)]
#[serde(deny_unknown_fields)]
struct Keyring {
    active: String,
    keys: std::collections::HashMap<String, String>,
    #[serde(default)]
    previous: Vec<String>,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Request {
    scope: String,
    tenant: Option<String>,
    namespace: String,
    value: String,
    key_version: Option<String>,
}
#[derive(Debug, Serialize, PartialEq, Eq)]
struct Token {
    token: String,
    key_version: String,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    rotation_aliases: Vec<TokenAlias>,
}
#[derive(Debug, Serialize, PartialEq, Eq)]
struct TokenAlias {
    token: String,
    key_version: String,
}

fn response(stream: &mut TcpStream, status: &str, body: &[u8]) {
    let header = format!(
        "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    let _ = stream.write_all(header.as_bytes());
    let _ = stream.write_all(body);
}

fn tokenize(keyring: &Keyring, request: Request) -> Result<Token, &'static str> {
    if ![
        "msisdn",
        "account",
        "device",
        "beneficiary",
        "agent",
        "wallet",
    ]
    .contains(&request.namespace.as_str())
    {
        return Err("invalid_namespace");
    }
    if request.value.is_empty()
        || request.value.len() > 512
        || request.value.trim() != request.value
    {
        return Err("invalid_value");
    }
    let tenant = match request.scope.as_str() {
        "network" => "network",
        "tenant" => request
            .tenant
            .as_deref()
            .filter(|v| !v.is_empty())
            .ok_or("tenant_required")?,
        _ => return Err("invalid_scope"),
    };
    let token_for = |version: &str| -> Result<String, &'static str> {
        let secret = keyring.keys.get(version).ok_or("key_version_unavailable")?;
        let key = hex::decode(secret).map_err(|_| "invalid_keyring")?;
        if key.len() != 32 {
            return Err("invalid_keyring");
        }
        let mut mac = HmacSha256::new_from_slice(&key).map_err(|_| "invalid_keyring")?;
        mac.update(b"kinga-token-v1\0");
        mac.update(request.scope.as_bytes());
        mac.update(b"\0");
        mac.update(tenant.as_bytes());
        mac.update(b"\0");
        mac.update(request.namespace.as_bytes());
        mac.update(b"\0");
        mac.update(request.value.as_bytes());
        Ok(hex::encode(mac.finalize().into_bytes()))
    };
    if let Some(version) = request.key_version.as_deref() {
        if version != keyring.active && !keyring.previous.iter().any(|old| old == version) {
            return Err("key_version_unavailable");
        }
        return Ok(Token {
            token: token_for(version)?,
            key_version: version.to_owned(),
            rotation_aliases: Vec::new(),
        });
    }
    let token = token_for(&keyring.active)?;
    let mut rotation_aliases = Vec::with_capacity(keyring.previous.len());
    for version in &keyring.previous {
        rotation_aliases.push(TokenAlias {
            token: token_for(version)?,
            key_version: version.clone(),
        });
    }
    Ok(Token {
        token,
        key_version: keyring.active.clone(),
        rotation_aliases,
    })
}

fn validate_keyring(keyring: &Keyring) -> Result<(), &'static str> {
    if !valid_key_id(&keyring.active)
        || keyring
            .previous
            .iter()
            .any(|version| !valid_key_id(version))
    {
        return Err("invalid_key_version");
    }
    if keyring.previous.contains(&keyring.active) {
        return Err("active_key_cannot_be_previous");
    }
    let mut versions = std::collections::HashSet::new();
    versions.insert(keyring.active.as_str());
    for version in &keyring.previous {
        if !versions.insert(version.as_str()) {
            return Err("duplicate_key_version");
        }
    }
    if keyring.keys.len() != versions.len()
        || keyring
            .keys
            .keys()
            .any(|version| !versions.contains(version.as_str()))
    {
        return Err("keyring_must_contain_only_active_and_previous_keys");
    }
    let mut key_material = std::collections::HashSet::new();
    for version in versions {
        let encoded = keyring.keys.get(version).ok_or("key_version_missing")?;
        let bytes = hex::decode(encoded).map_err(|_| "invalid_keyring")?;
        if bytes.len() != 32 || !key_material.insert(bytes) {
            return Err("invalid_or_reused_key_material");
        }
    }
    Ok(())
}

fn valid_key_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 64
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-' || byte == b'_')
}

fn handle(mut stream: TcpStream, keyring: Arc<Keyring>, caller_secret: Arc<Vec<u8>>) {
    let _ = stream.set_read_timeout(Some(std::time::Duration::from_secs(2)));
    let mut raw = Vec::new();
    let mut chunk = [0u8; 4096];
    loop {
        match stream.read(&mut chunk) {
            Ok(0) | Err(_) => return,
            Ok(n) => {
                raw.extend_from_slice(&chunk[..n]);
                if raw.windows(4).any(|w| w == b"\r\n\r\n") {
                    break;
                }
                if raw.len() > 8192 {
                    response(
                        &mut stream,
                        "413 Content Too Large",
                        b"{\"error\":\"headers_too_large\"}",
                    );
                    return;
                }
            }
        }
    }
    let header_end = raw.windows(4).position(|w| w == b"\r\n\r\n").unwrap() + 4;
    let headers = String::from_utf8_lossy(&raw[..header_end]);
    if headers.lines().next().unwrap_or("") == "GET /healthz HTTP/1.1" {
        response(&mut stream, "200 OK", b"{\"status\":\"ok\"}");
        return;
    }
    if !headers
        .lines()
        .next()
        .unwrap_or("")
        .starts_with("POST /v1/tokenize HTTP/")
    {
        response(&mut stream, "404 Not Found", b"{\"error\":\"not_found\"}");
        return;
    }
    let auth = headers
        .lines()
        .find_map(|line| {
            line.split_once(':')
                .filter(|(k, _)| k.eq_ignore_ascii_case("x-tokenizer-key"))
                .map(|(_, v)| v.trim())
        })
        .unwrap_or("");
    if !constant_time_equal(auth.as_bytes(), &caller_secret) {
        response(
            &mut stream,
            "401 Unauthorized",
            b"{\"error\":\"unauthorized\"}",
        );
        return;
    }
    let length = headers
        .lines()
        .find_map(|line| {
            line.split_once(':')
                .filter(|(k, _)| k.eq_ignore_ascii_case("content-length"))
                .and_then(|(_, v)| v.trim().parse::<usize>().ok())
        })
        .unwrap_or(0);
    if length == 0 || length > 4096 {
        response(
            &mut stream,
            "413 Content Too Large",
            b"{\"error\":\"invalid_body_size\"}",
        );
        return;
    }
    let mut body = raw[header_end..].to_vec();
    if body.len() > length {
        body.truncate(length);
    }
    while body.len() < length {
        let need = length - body.len();
        let read_len = need.min(chunk.len());
        match stream.read(&mut chunk[..read_len]) {
            Ok(0) | Err(_) => return,
            Ok(n) => body.extend_from_slice(&chunk[..n]),
        }
    }
    match serde_json::from_slice::<Request>(&body)
        .map_err(|_| "invalid_request")
        .and_then(|req| tokenize(&keyring, req))
    {
        Ok(value) => response(
            &mut stream,
            "200 OK",
            &serde_json::to_vec(&value).unwrap_or_default(),
        ),
        Err(error) => response(
            &mut stream,
            "422 Unprocessable Content",
            format!("{{\"error\":\"{error}\"}}").as_bytes(),
        ),
    }
}

fn constant_time_equal(a: &[u8], b: &[u8]) -> bool {
    let mut difference = a.len() ^ b.len();
    for i in 0..a.len().max(b.len()) {
        difference |= usize::from(*a.get(i).unwrap_or(&0) ^ *b.get(i).unwrap_or(&0));
    }
    difference == 0
}

fn main() {
    let raw = env::var("KINGA_TOKEN_KEYRING").expect("KINGA_TOKEN_KEYRING required");
    let keyring: Keyring = serde_json::from_str(&raw).expect("invalid token keyring JSON");
    validate_keyring(&keyring).expect("invalid tokenizer keyring configuration");
    let caller = env::var("KINGA_TOKENIZER_CALLER_KEY")
        .expect("KINGA_TOKENIZER_CALLER_KEY required")
        .into_bytes();
    if caller.len() < 32 || !keyring.keys.contains_key(&keyring.active) {
        panic!("invalid tokenizer credentials or active key");
    }
    let addr = env::var("KINGA_TOKENIZER_ADDR").unwrap_or_else(|_| "0.0.0.0:8091".into());
    let listener = TcpListener::bind(&addr).expect("cannot bind tokenizer");
    eprintln!("tokenizer listening on {addr}");
    let keyring = Arc::new(keyring);
    let caller = Arc::new(caller);
    for stream in listener.incoming().flatten() {
        let k = Arc::clone(&keyring);
        let c = Arc::clone(&caller);
        thread::spawn(move || handle(stream, k, c));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn test_keyring() -> Keyring {
        let mut keys = HashMap::new();
        // 32-byte hex keys
        keys.insert(
            "k1".into(),
            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef".into(),
        );
        keys.insert(
            "k2".into(),
            "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210".into(),
        );
        Keyring {
            active: "k1".into(),
            keys,
            previous: Vec::new(),
        }
    }

    #[test]
    fn test_tokenize_network_and_tenant_scope() {
        let kr = test_keyring();

        // Network scope
        let req_net = Request {
            scope: "network".into(),
            tenant: None,
            namespace: "msisdn".into(),
            value: "+254712345678".into(),
            key_version: None,
        };
        let tok_net = tokenize(&kr, req_net).expect("network tokenization failed");
        assert_eq!(tok_net.key_version, "k1");
        assert_eq!(tok_net.token.len(), 64);

        // Tenant scope (tenant A)
        let req_tenant_a = Request {
            scope: "tenant".into(),
            tenant: Some("tenantA".into()),
            namespace: "msisdn".into(),
            value: "+254712345678".into(),
            key_version: None,
        };
        let tok_tenant_a = tokenize(&kr, req_tenant_a).expect("tenantA tokenization failed");
        assert_eq!(tok_tenant_a.key_version, "k1");

        // Tenant scope (tenant B)
        let req_tenant_b = Request {
            scope: "tenant".into(),
            tenant: Some("tenantB".into()),
            namespace: "msisdn".into(),
            value: "+254712345678".into(),
            key_version: None,
        };
        let tok_tenant_b = tokenize(&kr, req_tenant_b).expect("tenantB tokenization failed");

        // Domain separation guarantees all three tokens are distinct
        assert_ne!(tok_net.token, tok_tenant_a.token);
        assert_ne!(tok_tenant_a.token, tok_tenant_b.token);
    }

    #[test]
    fn test_tokenize_key_rotation() {
        let mut kr = test_keyring();

        let req_k1 = Request {
            scope: "network".into(),
            tenant: None,
            namespace: "account".into(),
            value: "acc_100".into(),
            key_version: Some("k1".into()),
        };
        let tok_k1 = tokenize(&kr, req_k1).unwrap();
        kr.active = "k2".into();
        kr.previous = vec!["k1".into()];
        validate_keyring(&kr).unwrap();
        let tok_rotated = tokenize(
            &kr,
            Request {
                scope: "network".into(),
                tenant: None,
                namespace: "account".into(),
                value: "acc_100".into(),
                key_version: None,
            },
        )
        .unwrap();
        assert_eq!(tok_rotated.key_version, "k2");
        assert_eq!(tok_rotated.rotation_aliases.len(), 1);
        assert_eq!(tok_rotated.rotation_aliases[0].key_version, "k1");
        assert_ne!(tok_k1.token, tok_rotated.token);
        assert_eq!(tok_k1.token, tok_rotated.rotation_aliases[0].token);
    }

    #[test]
    fn test_keyring_rejects_unknown_and_reused_keys() {
        let mut kr = test_keyring();
        kr.keys.insert("unexpected".into(), "aa".repeat(32));
        assert_eq!(
            validate_keyring(&kr).unwrap_err(),
            "keyring_must_contain_only_active_and_previous_keys"
        );
        let mut kr = test_keyring();
        kr.keys.insert("k2".into(), kr.keys["k1"].clone());
        kr.previous.push("k2".into());
        assert_eq!(
            validate_keyring(&kr).unwrap_err(),
            "invalid_or_reused_key_material"
        );
    }

    #[test]
    fn test_tokenize_validation() {
        let kr = test_keyring();

        // Invalid namespace
        let req_bad_ns = Request {
            scope: "network".into(),
            tenant: None,
            namespace: "ssn".into(),
            value: "12345".into(),
            key_version: None,
        };
        assert_eq!(tokenize(&kr, req_bad_ns).unwrap_err(), "invalid_namespace");

        // Untrimmed value
        let req_untrimmed = Request {
            scope: "network".into(),
            tenant: None,
            namespace: "msisdn".into(),
            value: " +254712345678 ".into(),
            key_version: None,
        };
        assert_eq!(tokenize(&kr, req_untrimmed).unwrap_err(), "invalid_value");

        // Tenant required for tenant scope
        let req_no_tenant = Request {
            scope: "tenant".into(),
            tenant: None,
            namespace: "device".into(),
            value: "dvc_123".into(),
            key_version: None,
        };
        assert_eq!(tokenize(&kr, req_no_tenant).unwrap_err(), "tenant_required");
    }

    #[test]
    fn test_constant_time_equal() {
        assert!(constant_time_equal(b"secret123", b"secret123"));
        assert!(!constant_time_equal(b"secret123", b"secret124"));
        assert!(!constant_time_equal(b"secret", b"secret123"));
    }
}
