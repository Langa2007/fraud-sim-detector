# Runbook: evidence log integrity

1. Keep the service in shadow mode and stop it before copying evidence.
2. Preserve the original `data/evidence.jsonl` read-only and record its file hash in the incident notes.
3. On restart, the service validates each sequence number, previous hash and record hash. A mismatch prevents startup.
4. Do not edit or truncate the chain to restore service. Move the damaged file to a forensic copy and start a new evidence file only under incident lead direction.
5. The current local JSONL store is single-writer development storage; production needs a durable database/object store and independently anchored chain head.
