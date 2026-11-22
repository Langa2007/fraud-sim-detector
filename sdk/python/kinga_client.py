"""Minimal standard-library Kinga API client with signed requests."""
from __future__ import annotations

import hashlib
import hmac
import json
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urljoin, urlparse
from urllib.request import Request, urlopen


class KingaClient:
    def __init__(self, base_url: str, tenant: str, key_id: str, secret: str, timeout: float = 2.0):
        parsed = urlparse(base_url)
        if parsed.scheme not in ("http", "https") or not parsed.netloc:
            raise ValueError("base_url must be an absolute HTTP(S) URL")
        if not tenant or not key_id or len(secret.encode()) < 16:
            raise ValueError("tenant, key_id and at least 16 secret bytes are required")
        self.base_url = base_url.rstrip("/") + "/"
        self.tenant, self.key_id, self.secret, self.timeout = tenant, key_id, secret.encode(), timeout

    def assess(self, event: dict) -> dict:
        event_id = event.get("tenant_event_id")
        if not event_id:
            raise ValueError("event.tenant_event_id is required")
        return self._post("v1/assess", event, event_id)

    def tokenize(self, namespace: str, value: str, scope: str = "network") -> dict:
        return self._post("v1/tokenize", {"namespace": namespace, "value": value, "scope": scope})

    def _post(self, path: str, payload: dict, idempotency_key: str | None = None) -> dict:
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
        timestamp = str(int(time.time()))
        signature = hmac.new(self.secret, timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
        headers = {
            "Content-Type": "application/json", "X-Kinga-Key": self.key_id,
            "X-Kinga-Tenant": self.tenant, "X-Kinga-Timestamp": timestamp,
            "X-Kinga-Signature": signature,
        }
        if idempotency_key:
            headers["Idempotency-Key"] = idempotency_key
        request = Request(urljoin(self.base_url, path), body, headers, method="POST")
        try:
            with urlopen(request, timeout=self.timeout) as response:
                return json.loads(response.read(1 << 20))
        except HTTPError as exc:
            detail = exc.read(4096).decode("utf-8", errors="replace")
            raise RuntimeError(f"Kinga API returned HTTP {exc.code}: {detail}") from exc
        except URLError as exc:
            raise RuntimeError("Kinga API request failed") from exc
