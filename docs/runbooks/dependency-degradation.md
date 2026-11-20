# Runbook: assessment/evidence dependency degradation

1. Check `/healthz`, process logs and available disk space. `/metrics` exposes the minimal build metric; deeper OpenTelemetry/Prometheus instrumentation is not yet wired.
2. A failed evidence append returns `503`; the service does not return a decision without an evidence record.
3. An invalid HMAC returns `401`; verify tenant key configuration and client clock (±5 minutes).
4. An idempotency `409` means the same tenant event ID was reused with changed content. Investigate the caller; do not generate a new ID merely to bypass this guard.
5. If evidence integrity prevents startup, preserve the file and follow `evidence-integrity.md`.
