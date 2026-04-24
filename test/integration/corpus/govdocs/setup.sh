#!/usr/bin/env bash
# setup.sh: Govdocs corpus generation entrypoint.
#
# Downloads a fixed set of Govdocs1 thread archives, extracts the PDFs,
# builds PDF.js from source for reference generation, then runs both
# PDF.js (per PDF) and VeraPDF (per profile) to emit goldens.
set -euo pipefail

OUTPUT_DIR="/output"
PDFS_DIR="$OUTPUT_DIR/pdfs"
TEXT_DIR="$OUTPUT_DIR/goldens/text"
META_DIR="$OUTPUT_DIR/goldens/metadata"
COMP_DIR="$OUTPUT_DIR/goldens/compliance"

mkdir -p "$PDFS_DIR" "$TEXT_DIR" "$META_DIR" "$COMP_DIR"

echo "==> Downloading Govdocs1 threads..."
/work/download-threads.sh "$PDFS_DIR"

TOTAL=$(find "$PDFS_DIR" -maxdepth 1 -name '*.pdf' | wc -l)
echo "==> $TOTAL PDFs extracted."
if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no PDFs were extracted from the requested threads." >&2
  exit 1
fi

echo "==> Cloning pdf.js..."
git clone --depth 1 https://github.com/mozilla/pdf.js.git /tmp/pdf.js

echo "==> Installing pdf.js dependencies..."
(cd /tmp/pdf.js && npm install)

echo "==> Building pdf.js (legacy build for Node.js)..."
(cd /tmp/pdf.js && npx gulp generic-legacy)

echo "==> Generating PDF.js text/metadata goldens..."
node /work/gen-references.mjs \
  --pdfjs-dir /tmp/pdf.js \
  --pdfs-dir "$PDFS_DIR" \
  --text-out "$TEXT_DIR" \
  --meta-out "$META_DIR"

echo "==> Running VeraPDF for every profile..."
/work/run-verapdf.sh "$PDFS_DIR" "$COMP_DIR"

echo "==> Done."
