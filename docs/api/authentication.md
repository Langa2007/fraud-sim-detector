# Assessment request signing

Each request uses a tenant-bound API key and HMAC-SHA256 signature. Configure credentials as comma-separated `tenant:key-id:secret` triples in `KINGA_API_KEYS`. Keep secrets outside source control and use separate secrets per client in any shared environment.

The signature is the lowercase hexadecimal HMAC of the exact UTF-8 body bytes, prefixed by the Unix timestamp and a dot:

```text
hex(HMAC_SHA256(secret, timestamp + "." + exact_body_bytes))
```

Send `X-Kinga-Key`, `X-Kinga-Timestamp`, `X-Kinga-Signature`, `Idempotency-Key`, and `Content-Type: application/json`. Timestamp skew may not exceed five minutes. `Idempotency-Key` must exactly equal the event's `tenant_event_id`. A same-key/same-body replay returns the original response; same-key/different-body returns `409`. The in-memory cache resets on process restart, so callers should retain their own event IDs and retry only with the original body.

Use opaque, non-personal event IDs. Never put a phone number, account number, name or other personal data into `tenant_event_id`, because the identifier is retained in the evidence chain.

The current gateway accepts up to 300 requests per tenant in each rolling minute window. This is a fixed-window, in-process protection for the single-instance Phase 1 starter, not a distributed quota service.
