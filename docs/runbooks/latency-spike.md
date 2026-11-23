# Gateway + Redis latency spike

This local spike exercises the Go assessment gateway with Redis-backed idempotency and a deterministic stub scorer. It reports end-to-end request latency and throughput; it does not establish a production SLO or fraud-scoring performance.

## Start the spike stack

Use disposable local credentials only. The following values are intentionally public development values and must never be reused outside this local experiment:

```sh
KINGA_API_KEYS='spike:spike-key:spike-secret-change-me' \
KINGA_TOKENIZER_CALLER_KEY='local-spike-caller-key-change-before-use-0001' \
KINGA_TOKEN_KEYRING='{"active":"spike-v1","previous":[],"keys":{"spike-v1":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}' \
KINGA_CONSORTIUM_ADMIN_TOKEN='local-spike-admin-token-change-before-use-0001' \
KINGA_ENGINE_URL='http://stub-scorer:8092' \
docker compose --profile latency-spike up --build -d
```

Wait for `kinga`, `redis`, `tokenizer`, and `stub-scorer` to be healthy/running. The stub always returns a low-risk `ALLOW` response and supports an optional fixed delay with `-delay`; it is not a decision engine. Redis is internal to the Compose network and stores only hashed idempotency keys, body hashes, and cached response JSON with the configured TTL.

## Run the signed latency workload

From the repository root:

```sh
go run ./tools/latency-spike/load --url http://127.0.0.1:8080/v1/assess --count 100 --concurrency 20 --tenant spike --key-id spike-key --secret spike-secret-change-me
```

The runner creates unique event IDs, signs each exact request body using the gateway HMAC contract, and reports throughput, failures, and p50/p95/p99/max end-to-end latency. The gateway limits each tenant to 300 assessments per minute, so keep `--count` at or below 300. Run multiple rounds and record the machine/CPU limits, request count, concurrency, scorer delay, and Redis mode beside the output. Compare against the in-memory path by restarting Compose with `KINGA_REDIS_URL=''`; the application then uses its process-local cache.

Stop the local services when finished:

```sh
docker compose --profile latency-spike down
```

This spike keeps idempotency results in Redis, but the assessment key lock remains process-local. It does not prove cross-replica exactly-once behavior, production Redis failover, or performance under partner traffic. Those require a separate distributed-concurrency design and representative deployment/load conditions.
