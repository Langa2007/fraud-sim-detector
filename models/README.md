# Model artifacts

`tools/ml/train.py` writes `current/model.onnx`, `current/model.json`, and `current/evaluation.json` here. Generated models and evaluation reports are ignored by Git because they are tenant-data artifacts. Keep published tenant reports in the customer's approved evidence store.

The engine loads ONNX only when `KINGA_MODEL_PATH` is set. Mount this directory read-only in Docker Compose, then set `KINGA_MODEL_PATH=/app/models/current/model.onnx`. The card is loaded from the same directory as `model.json` unless `KINGA_MODEL_CARD_PATH` is set. A missing or invalid configured model fails engine startup; with no model path, the rules-only shadow engine remains available.
