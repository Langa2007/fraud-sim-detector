build:
    go build -o /tmp/kinga ./services/gateway/cmd
    cargo build --manifest-path engine/Cargo.toml --release

test:
    go test ./...

lint:
    go vet ./...

up:
    docker compose up --build

engine:
    KINGA_POLICY_PATH=policies/default.json cargo run --manifest-path engine/Cargo.toml

replay events:
    python3 tools/replay/replay.py {{events}}

ingest source destination:
    python3 tools/ingest/csv_to_jsonl.py {{source}} {{destination}}
    python3 tools/replay/replay.py {{destination}}

synth count="1000" out="synthetic.jsonl":
    python3 tools/synth/generate.py --count {{count}} --out {{out}}
