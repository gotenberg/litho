#!/usr/bin/env bash
# scripts/fetch-pdfjs-corpus.sh
#
# Populates pkg/pdf/testdata/pdf.js/ with test PDFs, manifest, and reference
# JSON fixtures from the Mozilla PDF.js project.
#
# Usage:
#   bash scripts/fetch-pdfjs-corpus.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CORPUS_DIR="$ROOT_DIR/test/integration/testdata/pdf.js"
TMP_DIR="$ROOT_DIR/tmp"

trap 'rm -rf "$TMP_DIR"' EXIT

echo "==> Cloning pdf.js into temp directory..."
git clone --depth 1 https://github.com/mozilla/pdf.js.git "$TMP_DIR/pdf.js"

echo "==> Installing dependencies..."
(cd "$TMP_DIR/pdf.js" && npm install)

echo "==> Downloading test PDFs (Tier 2 — external URLs)..."
# Custom parallel downloader — bypasses test.mjs which hangs on MD5
# mismatches (pdf.js bug #19580). Downloads up to 8 files concurrently
# with a 30s per-file timeout to prevent hangs from dead links.
# Files with MD5 mismatches are deleted.
node "$ROOT_DIR/scripts/download-pdfjs-pdfs.mjs" \
  --test-dir "$TMP_DIR/pdf.js/test" \
  || echo "WARN: download step failed"

echo "==> Copying corpus..."
mkdir -p "$CORPUS_DIR/pdfs"
cp "$TMP_DIR/pdf.js/test/test_manifest.json" "$CORPUS_DIR/"

# Copy every PDF that was successfully obtained (Tier 1 + Tier 2).
# Skip .error files left by failed downloads.
find "$TMP_DIR/pdf.js/test/pdfs" -name '*.pdf' -exec cp {} "$CORPUS_DIR/pdfs/" \;

TOTAL=$(find "$CORPUS_DIR/pdfs" -name '*.pdf' | wc -l)
echo "==> $TOTAL PDFs available."

# Count how many manifest entries reference PDFs we don't have.
MANIFEST_COUNT=$(python3 -c "
import json, os
m = json.load(open('$CORPUS_DIR/test_manifest.json'))
files = {e.get('file','') for e in m}
missing = [f for f in files if not os.path.exists(os.path.join('$CORPUS_DIR', f))]
print(len(missing))
" 2>/dev/null || echo "?")
echo "==> $MANIFEST_COUNT manifest entries have no local PDF (Tier 3 dead links)."

echo "==> Building pdf.js from source (legacy build for Node.js)..."
(cd "$TMP_DIR/pdf.js" && npx gulp generic-legacy)

echo "==> Generating reference fixtures..."
node "$ROOT_DIR/scripts/gen-pdfjs-reference.mjs" \
  --pdfjs-dir "$TMP_DIR/pdf.js" \
  --manifest "$CORPUS_DIR/test_manifest.json" \
  --out "$CORPUS_DIR/references/"

echo "==> Done."
