#!/usr/bin/env python3
"""Fail if the shared feature schema drifts from Python training or Rust serving."""
import ast
import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
schema = json.loads((ROOT / "schemas/features/v1.json").read_text(encoding="utf-8"))
expected = [feature["name"] for feature in schema["features"]]

python_tree = ast.parse((ROOT / "tools/ml/train.py").read_text(encoding="utf-8"))
python_names = None
for node in python_tree.body:
    if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == "FEATURE_NAMES" for target in node.targets):
        python_names = ast.literal_eval(node.value)
        break

rust = (ROOT / "engine/src/main.rs").read_text(encoding="utf-8")
match = re.search(r"const FEATURE_ORDER: \[&str; \d+\] = \[(.*?)\];", rust, re.S)
rust_names = re.findall(r'"([a-z0-9_]+)"', match.group(1)) if match else None

if python_names != expected or rust_names != expected:
    raise SystemExit(json.dumps({"schema": expected, "python": python_names, "rust": rust_names}, indent=2))
print(f"feature contract aligned: {len(expected)} fields ({schema['schema_version']})")
