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

#[derive(Deserialize)]
struct Keyring {
    active: String,
    keys: std::collections::HashMap<String, String>,
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
#[derive(Serialize)]
struct Token {
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
    let version = request.key_version.as_deref().unwrap_or(&keyring.active);
    let secret = keyring.keys.get(version).ok_or("key_version_unavailable")?;
    let key = hex::decode(secret).map_err(|_| "invalid_keyring")?;
    if key.len() < 32 {
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
    Ok(Token {
        token: hex::encode(mac.finalize().into_bytes()),
        key_version: version.to_owned(),
    })
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
