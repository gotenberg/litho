#!/usr/bin/env bash
# setup.sh — Corpus generation entrypoint for the testcontainer.
#
# Clones Mozilla PDF.js, downloads test PDFs, builds PDF.js, and generates
# reference JSON fixtures. Output is written to /output (bind-mounted from host).
set -euo pipefail

OUTPUT_DIR="/output"

echo "==> Cloning pdf.js..."
git clone --depth 1 https://github.com/mozilla/pdf.js.git /tmp/pdf.js

echo "==> Installing dependencies..."
(cd /tmp/pdf.js && npm install)

echo "==> Downloading test PDFs (Tier 2 — external URLs)..."
node /work/download-pdfjs-pdfs.mjs --test-dir /tmp/pdf.js/test \
  || echo "WARN: download step had failures (some dead links are expected)"

echo "==> Copying corpus to output..."
mkdir -p "$OUTPUT_DIR/pdfs"
cp /tmp/pdf.js/test/test_manifest.json "$OUTPUT_DIR/"
find /tmp/pdf.js/test/pdfs -name '*.pdf' -exec cp {} "$OUTPUT_DIR/pdfs/" \;

TOTAL=$(find "$OUTPUT_DIR/pdfs" -name '*.pdf' | wc -l)
echo "==> $TOTAL PDFs available."

echo "==> Building pdf.js from source (legacy build for Node.js)..."
(cd /tmp/pdf.js && npx gulp generic-legacy)

echo "==> Generating reference fixtures..."
node /work/gen-pdfjs-reference.mjs \
  --pdfjs-dir /tmp/pdf.js \
  --manifest "$OUTPUT_DIR/test_manifest.json" \
  --out "$OUTPUT_DIR/references/"

echo "==> Done."
