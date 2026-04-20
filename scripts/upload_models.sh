#!/usr/bin/env bash
# scripts/upload_models.sh
#
# Step 1 — train & export (requires Python + PyTorch):
#   pip install transformers torch onnx onnxruntime sentencepiece huggingface-hub
#   python scripts/export_onnx.py --out-dir artifacts
#
# Step 2 — upload to the latest GitHub Release (requires gh CLI + auth):
#   bash scripts/upload_models.sh
#
# The NSIS installer already fetches from:
#   /releases/latest/download/netmonit-classifier.onnx
#   /releases/latest/download/spiece.model
# so uploading to the "Latest" release is sufficient.

set -euo pipefail

ONNX_FILE="${1:-artifacts/netmonit-classifier.onnx}"
SPM_FILE="${2:-artifacts/spiece.model}"
RELEASE_TAG="${3:-}"   # leave empty to upload to the current Latest release

if [[ ! -f "$ONNX_FILE" ]]; then
    echo "ERROR: $ONNX_FILE not found."
    echo "Run:  python scripts/export_onnx.py --out-dir artifacts"
    exit 1
fi

if [[ ! -f "$SPM_FILE" ]]; then
    echo "ERROR: $SPM_FILE not found."
    echo "Run:  python scripts/export_onnx.py --out-dir artifacts"
    exit 1
fi

ONNX_MB=$(du -m "$ONNX_FILE" | cut -f1)
SPM_KB=$(du -k  "$SPM_FILE"  | cut -f1)
echo "Ready to upload:"
echo "  $ONNX_FILE  (${ONNX_MB} MB)"
echo "  $SPM_FILE   (${SPM_KB} KB)"

# Resolve tag: use provided arg or fall back to the latest release tag
if [[ -z "$RELEASE_TAG" ]]; then
    RELEASE_TAG=$(gh release list --limit 1 --json tagName --jq '.[0].tagName')
    echo "Uploading to latest release: $RELEASE_TAG"
else
    echo "Uploading to release: $RELEASE_TAG"
fi

# --clobber replaces existing assets of the same name (safe to re-run)
gh release upload "$RELEASE_TAG" \
    "$ONNX_FILE" \
    "$SPM_FILE"  \
    --clobber

echo ""
echo "Done. Assets live at:"
echo "  https://github.com/hadinurhakim-coding/net-monit/releases/download/${RELEASE_TAG}/netmonit-classifier.onnx"
echo "  https://github.com/hadinurhakim-coding/net-monit/releases/download/${RELEASE_TAG}/spiece.model"
