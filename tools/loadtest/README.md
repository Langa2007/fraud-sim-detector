# Kinga Load Test Scenarios

This directory contains load test scenarios using [k6](https://k6.io/) to validate Phase 1 performance targets:

- **Target:** p99 `POST /v1/assess` latency $\le$ 150 ms at target RPS.
- **Error Budget:** HTTP failure rate $<$ 0.1%.

## Running the Load Test

Ensure both services are running:
```bash
docker compose up -d
```

Execute the k6 test:
```bash
k6 run tools/loadtest/k6_assess.js
```

Or configure custom endpoints and credentials:
```bash
KINGA_GATEWAY_URL="http://localhost:8080" \
KINGA_API_KEY="demo-key" \
KINGA_API_SECRET="demo-secret" \
k6 run tools/loadtest/k6_assess.js
```
