#!/usr/bin/env python3
"""Train a time-split, calibrated fraud model and export ONNX plus an audit report."""
import argparse
import json
import math
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
from sklearn.ensemble import GradientBoostingClassifier
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import average_precision_score, confusion_matrix, precision_score


FEATURE_NAMES = [
    "amount_kes", "sim_swap_age_minutes", "sim_swap_known", "device_first_seen",
    "beneficiary_first_seen", "balance_drain_ratio", "transfers_in_window",
    "new_beneficiaries_in_window", "window_minutes", "account_age_days",
    "account_age_known", "inbound_fan_in", "rapid_outbound", "overnight",
    "is_funds_transfer",
]


def event_time(event):
    occurred = datetime.fromisoformat(event["occurred_at"].replace("Z", "+00:00"))
    if occurred.tzinfo is None:
        raise ValueError("occurred_at must include an RFC3339 timezone offset")
    return occurred


def features(event):
    signals = event.get("signals") or {}
    transaction = event.get("transaction") or {}
    occurred = event_time(event)
    hour = occurred.hour
    swap = signals.get("sim_swap_age_minutes")
    age = signals.get("account_age_days")
    values = {
        "amount_kes": transaction.get("amount", 0),
        "sim_swap_age_minutes": swap or 0,
        "sim_swap_known": swap is not None,
        "device_first_seen": signals.get("device_first_seen", False),
        "beneficiary_first_seen": signals.get("beneficiary_first_seen", False),
        "balance_drain_ratio": signals.get("balance_drain_ratio", 0),
        "transfers_in_window": signals.get("transfers_in_window", 0),
        "new_beneficiaries_in_window": signals.get("new_beneficiaries_in_window", 0),
        "window_minutes": signals.get("window_minutes", 0),
        "account_age_days": age or 0,
        "account_age_known": age is not None,
        "inbound_fan_in": signals.get("inbound_fan_in", 0),
        "rapid_outbound": signals.get("rapid_outbound", False),
        "overnight": hour >= 22 or hour < 5,
        "is_funds_transfer": event.get("event_type") == "funds_transfer",
    }
    return [float(values[name]) for name in FEATURE_NAMES]


def read_dataset(path):
    events, labels = [], []
    with Path(path).open(encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if not line.strip():
                continue
            event = json.loads(line)
            if not isinstance(event.get("confirmed_fraud"), bool):
                raise ValueError(f"line {line_number}: confirmed_fraud must be a boolean")
            events.append(event)
            labels.append(int(event["confirmed_fraud"]))
    if len(events) < 60 or len(set(labels)) != 2:
        raise ValueError("need at least 60 labelled events with both fraud and legitimate examples")
    order = sorted(range(len(events)), key=lambda i: event_time(events[i]).timestamp())
    x = np.asarray([features(events[i]) for i in order], dtype=np.float32)
    y = np.asarray([labels[i] for i in order], dtype=np.int64)
    sorted_events = [events[i] for i in order]
    train_end, calibration_end = int(len(y) * 0.60), int(len(y) * 0.80)
    splits = (slice(0, train_end), slice(train_end, calibration_end), slice(calibration_end, None))
    for name, part in zip(("train", "calibration", "test"), splits):
        if len(set(y[part])) != 2:
            raise ValueError(f"time-based {name} split must contain fraud and legitimate labels")
    return x, y, sorted_events, splits


def calibrate(raw, labels):
    clipped = np.clip(raw, 1e-6, 1 - 1e-6)
    logits = np.log(clipped / (1 - clipped)).reshape(-1, 1)
    model = LogisticRegression(C=1e6, solver="lbfgs", random_state=7)
    model.fit(logits, labels)
    return float(model.coef_[0, 0]), float(model.intercept_[0])


def apply_calibration(raw, slope, intercept):
    clipped = np.clip(raw, 1e-6, 1 - 1e-6)
    logits = slope * np.log(clipped / (1 - clipped)) + intercept
    return 1 / (1 + np.exp(-np.clip(logits, -35, 35)))


def threshold_at_fpr(scores, labels, max_fpr):
    negatives = scores[labels == 0]
    if not len(negatives):
        raise ValueError("threshold calibration requires legitimate outcomes")
    candidates = np.unique(scores)[::-1]
    valid = [float(t) for t in candidates if float(np.mean(negatives >= t)) <= max_fpr]
    return min(valid) if valid else float(np.nextafter(max(scores), math.inf))


def operating_metrics(scores, labels, threshold):
    predicted = scores >= threshold
    tn, fp, fn, tp = confusion_matrix(labels, predicted, labels=[0, 1]).ravel()
    return {
        "threshold": float(threshold), "recall": float(tp / (tp + fn)) if tp + fn else None,
        "false_positive_rate": float(fp / (fp + tn)) if fp + tn else None,
        "precision": float(precision_score(labels, predicted, zero_division=0)),
        "true_positives": int(tp), "false_positives": int(fp),
        "false_negatives": int(fn), "true_negatives": int(tn),
    }


def cost_loss(events, labels, scores, threshold, false_positive_cost_ratio):
    loss_fraud, friction = 0.0, 0.0
    for event, label, score in zip(events, labels, scores):
        amount = float((event.get("transaction") or {}).get("amount", 0))
        if label == 1 and score < threshold:
            loss_fraud += float(event.get("outcome_loss_kes", amount))
        elif label == 0 and score >= threshold:
            friction += amount * false_positive_cost_ratio
    return {"currency": "KES", "missed_fraud_loss": loss_fraud,
            "legitimate_friction_cost": friction, "total": loss_fraud + friction,
            "false_positive_cost_ratio": false_positive_cost_ratio}


def psi(reference, current, bins=10):
    edges = np.unique(np.quantile(reference, np.linspace(0, 1, bins + 1)))
    if len(edges) < 3:
        return 0.0
    edges[0], edges[-1] = -np.inf, np.inf
    ref = np.histogram(reference, bins=edges)[0] / len(reference)
    cur = np.histogram(current, bins=edges)[0] / len(current)
    ref, cur = np.clip(ref, 1e-6, 1), np.clip(cur, 1e-6, 1)
    return float(np.sum((cur - ref) * np.log(cur / ref)))


def segment_report(events, labels, scores, threshold):
    report = {}
    for dimension in ("channel", "event_type"):
        values = sorted({event.get(dimension, "unknown") for event in events})
        for value in values:
            indices = [i for i, event in enumerate(events) if event.get(dimension, "unknown") == value]
            ys = labels[indices]
            if len(ys) < 5 or len(set(ys)) < 2:
                report[f"{dimension}:{value}"] = {"events": len(indices), "status": "insufficient_labels"}
            else:
                report[f"{dimension}:{value}"] = {"events": len(indices), **operating_metrics(scores[indices], ys, threshold)}
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dataset", help="Labelled canonical event JSONL")
    parser.add_argument("--out", default="models/current", help="Directory for ONNX model and reports")
    parser.add_argument("--max-fpr", type=float, default=0.005)
    parser.add_argument("--false-positive-cost-ratio", type=float, default=0.05)
    args = parser.parse_args()
    if not 0 < args.max_fpr < 1 or not 0 <= args.false_positive_cost_ratio <= 1:
        parser.error("max-fpr must be in (0,1), and false-positive-cost-ratio in [0,1]")

    x, y, events, (train, calibration, test) = read_dataset(args.dataset)
    estimator = GradientBoostingClassifier(n_estimators=120, learning_rate=0.05,
        max_depth=2, min_samples_leaf=10, random_state=7)
    estimator.fit(x[train], y[train])
    slope, intercept = calibrate(estimator.predict_proba(x[calibration])[:, 1], y[calibration])
    calibration_scores = apply_calibration(estimator.predict_proba(x[calibration])[:, 1], slope, intercept)
    test_scores = apply_calibration(estimator.predict_proba(x[test])[:, 1], slope, intercept)
    threshold = threshold_at_fpr(calibration_scores, y[calibration], args.max_fpr)
    operating = operating_metrics(test_scores, y[test], threshold)

    output = Path(args.out)
    output.mkdir(parents=True, exist_ok=True)
    try:
        from skl2onnx import convert_sklearn
        from skl2onnx.common.data_types import FloatTensorType
        import onnx
    except ImportError as exc:
        raise SystemExit("Install the model dependencies with: python -m pip install -r requirements-ml.txt") from exc

    onnx_model = convert_sklearn(estimator,
        initial_types=[("float_input", FloatTensorType([None, len(FEATURE_NAMES)]))],
        options={id(estimator): {"zipmap": False}}, target_opset=17)
    onnx.checker.check_model(onnx_model)
    onnx.save_model(onnx_model, output / "model.onnx")

    train_indices = np.arange(*train.indices(len(y)))
    calibration_indices = np.arange(*calibration.indices(len(y)))
    test_indices = np.arange(*test.indices(len(y)))
    model_version = "gbt-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    card = {
        "model_version": model_version, "feature_schema_version": "kinga.features.v1",
        "feature_order": FEATURE_NAMES, "input_name": "float_input",
        "calibration": {"method": "platt_scaling", "slope": slope, "intercept": intercept},
        "operating_threshold": threshold, "target_max_false_positive_rate": args.max_fpr,
        "training_window": {"first": events[train.start]["occurred_at"], "last": events[train.stop - 1]["occurred_at"]},
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "training_rows": int(len(train_indices)),
    }
    (output / "model.json").write_text(json.dumps(card, indent=2) + "\n", encoding="utf-8")

    report = {
        "model_version": model_version, "feature_schema_version": card["feature_schema_version"],
        "dataset": {"events": int(len(y)), "fraud": int(y.sum()), "legitimate": int(len(y) - y.sum()),
                    "train": int(len(train_indices)), "calibration": int(len(calibration_indices)),
                    "held_out_test": int(len(test_indices))},
        "split": "chronological 60% train / 20% calibration / 20% held-out test",
        "calibration": card["calibration"], "target_max_false_positive_rate": args.max_fpr,
        "held_out_metrics": {**operating, "pr_auc": float(average_precision_score(y[test], test_scores)),
                             "cost_weighted_loss": cost_loss(events[test.start:], y[test], test_scores, threshold, args.false_positive_cost_ratio)},
        "segments": segment_report(events[test.start:], y[test], test_scores, threshold),
        "feature_population_stability_index": {name: psi(x[train, i], x[test, i]) for i, name in enumerate(FEATURE_NAMES)},
        "limitations": ["Evaluation is valid only for the supplied tenant data and label process.",
                        "The threshold is selected on calibration data and measured once on the later test window.",
                        "Small segment counts are reported as insufficient_labels; they are not evidence of fairness."],
    }
    (output / "evaluation.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"model_version": model_version, "onnx": str(output / "model.onnx"),
                      "evaluation": str(output / "evaluation.json"), "held_out_metrics": report["held_out_metrics"]}, indent=2))


if __name__ == "__main__":
    main()
