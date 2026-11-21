build:
    go build -o /tmp/kinga ./services/gateway/cmd
    go build -o /tmp/kinga-ingest ./services/ingest/cmd
    go build -o /tmp/evidence-verify ./tools/evidence-verify
    cargo build --manifest-path engine/Cargo.toml --release

test:
    go test -v ./...
    cargo test --manifest-path engine/Cargo.toml

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

ingest-go source destination:
    go run ./services/ingest/cmd {{source}} {{destination}}

verify-evidence path="data/evidence.jsonl":
    go run ./tools/evidence-verify {{path}}

synth count="1000" out="synthetic.jsonl":
    python3 tools/synth/generate.py --count {{count}} --out {{out}}

check-features:
    python3 tools/ml/check_feature_contract.py

prepare-dataset events outcomes out tenant="demo":
    python3 tools/ml/prepare_dataset.py {{events}} {{outcomes}} {{out}} --tenant {{tenant}}

train data="data/labelled.jsonl" out="models/current":
    python3 tools/ml/train.py {{data}} --out {{out}}
