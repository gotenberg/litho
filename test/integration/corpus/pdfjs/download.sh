#!/usr/bin/env bash
# pdfjs/download.sh: fetch the PDF.js test corpus into a flat pdfs/
# directory. Re-uses the pdf.js clone baked into /opt/pdfjs so only the
# external-URL downloads (.link sidecars) run at provisioning time.
#
# Usage: pdfjs/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
TEST_DIR="/opt/pdfjs/test"

# Prime the corpus: pdf.js ships many PDFs inline; external-URL ones
# come from .link sidecars. Our helper handles concurrency + timeouts
# since pdf.js's own test.mjs hangs on MD5 mismatches.
node /work/pdfjs/download-pdfjs-pdfs.mjs --test-dir "$TEST_DIR" \
  || echo "WARN: download had failures (dead links expected)"

# Flatten any nested pdfs/ layout into OUT. pdf.js keeps everything
# under test/pdfs/ and a few subdirectories with sub-folder fixtures.
find "$TEST_DIR/pdfs" -type f -name '*.pdf' | while read -r pdf; do
  cp "$pdf" "$OUT/"
done

# Ship the fixture manifest too: litho's manifest.json declares Skips
# keyed on the PDF.js fixture ID (filename without extension), so
# callers can cross-reference.
cp "$TEST_DIR/test_manifest.json" "/output/test_manifest.json" || true
