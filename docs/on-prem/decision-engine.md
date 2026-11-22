# On-prem decision engine package

The Rust `kinga-engined` release binary and `engine/Dockerfile` are the on-prem decision-core package. It serves the private `/internal/v1/assess` contract and reads a policy file plus optional ONNX model/card at startup. The default policy is shadow mode. Do not expose port 8090 to an untrusted network: the private engine endpoint is intended to sit behind the tenant's authenticated gateway.

Build and run:

```sh
cargo build --locked --release --manifest-path engine/Cargo.toml
KINGA_POLICY_PATH=policies/default.json \
KINGA_ENGINE_ADDR=127.0.0.1:8090 \
engine/target/release/kinga-engined
```

For a container deployment, pin the image by digest, mount policy/model files read-only, provide the model and model-card paths through secrets/configuration, disable outbound network access unless explicitly required, and collect logs without event payloads. The current package is a private HTTP/JSON daemon, not a stable Rust library ABI or gRPC service; customer deployment requires a signed artifact, support window, and institution-specific security review.
