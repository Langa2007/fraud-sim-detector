# Kinga API clients

Small clients in Go, Python, and modern JavaScript share the public request-signing contract: HMAC-SHA256 over `unix_timestamp + "." + exact_json_body`; send the hex digest in `X-Kinga-Signature` alongside `X-Kinga-Key`, `X-Kinga-Timestamp`, and `X-Kinga-Tenant`. Assess requests also send `Idempotency-Key` matching `tenant_event_id`.

- Go package: `sdk/go/kinga` (standard library only)
- Python module: `sdk/python/kinga_client.py` (Python 3.10+, standard library only)
- JavaScript ESM: `sdk/js/index.js` (Node.js 18+, built-in `node:crypto` and `fetch`; no TypeScript)

Pass secrets from a secret manager at runtime. Do not commit credentials or log request bodies. The SDKs return response JSON and surface non-2xx statuses as errors; retry only with the same tenant event ID and exactly the same request body.
