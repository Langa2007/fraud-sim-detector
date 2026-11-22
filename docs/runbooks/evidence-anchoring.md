# Evidence chain anchoring

`tools/evidence-anchor` verifies the local evidence chain and signs its current sequence and head with Ed25519. The resulting append-only anchor is independently verifiable and can be copied to a retention-locked object store or an external timestamping service.

Create a keypair on an offline host and keep the private key outside the application host. The tool writes raw Ed25519 key bytes; private key permissions are 0600 and key creation refuses to overwrite existing files. Use a distinct `-key-id` for every key generation.

```sh
go run ./tools/evidence-anchor keygen \
  -private-key /offline/keys/evidence-2026-01.key \
  -public-key /offline/keys/evidence-2026-01.pub

go run ./tools/evidence-anchor anchor \
  -evidence /data/evidence.jsonl \
  -private-key /secure/anchor-ed25519.key \
  -key-id evidence-anchor-2026-01 \
  -output /secure-archive/evidence-anchors.jsonl

go run ./tools/evidence-anchor verify \
  -anchors /secure-archive/evidence-anchors.jsonl \
  -public-keys-dir /secure/archive-of-public-keys
```

Place each retained public key at `<public-keys-dir>/<key-id>.pub` to verify archives across key rotation. `-public-key` is available for single-key archives.

Run on a schedule appropriate to the evidence RPO, export each anchor off-host, and alert on missing scheduled anchors. This tool does not timestamp externally or prevent an operator with both the application data and signing key from producing a new chain; use an independent key custodian and immutable external storage for production assurance. Maintain prior public keys for verification after key rotation.
