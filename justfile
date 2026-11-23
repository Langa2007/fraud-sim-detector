build:
    go build -o /tmp/kinga ./services/gateway/cmd
    go build -o /tmp/kinga-ingest ./services/ingest/cmd
    go build -o /tmp/evidence-verify ./tools/evidence-verify
    cargo build --manifest-path engine/Cargo.toml --release
    cargo build --manifest-path services/tokenizer/Cargo.toml --release

gen:
    buf generate --template proto/buf.gen.yaml proto

lint:
    test -z "$(gofmt -l $(git ls-files '*.go'))"
    go vet ./...
    cargo fmt --manifest-path engine/Cargo.toml -- --check
    cargo fmt --manifest-path services/tokenizer/Cargo.toml -- --check
    cargo clippy --locked --manifest-path engine/Cargo.toml -- -D warnings
    cargo clippy --locked --manifest-path services/tokenizer/Cargo.toml -- -D warnings
    ruff check tools sdk/python
    node --check sdk/js/index.js
    node --check console/src/main.js
    buf format --diff --exit-code proto
    buf lint proto
    buf build proto

test:
    go test -v ./...
    cargo test --manifest-path engine/Cargo.toml
    cargo test --manifest-path services/tokenizer/Cargo.toml

up:
    docker compose up --build

engine:
    KINGA_POLICY_PATH=policies/default.json cargo run --manifest-path engine/Cargo.toml

tokenizer:
    cargo run --manifest-path services/tokenizer/Cargo.toml

build-graph input="data/synthetic.jsonl" output="data/graph_features.jsonl":
    python3 tools/graph/build_graph.py --input {{input}} --output {{output}}

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
