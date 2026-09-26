#!/usr/bin/env bash
# pdfjs/download.sh: fetch the PDF.js test corpus into a flat pdfs/
# directory. Re-uses the pdf.js checkout baked into /opt/pdfjs (pinned by
# PDFJS_REVISION in versions.env) so only the external-URL downloads
# (.link sidecars, MD5-checked against the test manifest) run at
# provisioning time.
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
# Plain shell redirect is used instead of cp/install because both of
# those call fallocate at the tail of the copy; macOS Docker Desktop's
# virtiofs bind mount rejects FALLOC_FL_PUNCH_HOLE with EINVAL and the
# tool exits non-zero on files it copied successfully.
find "$TEST_DIR/pdfs" -type f -name '*.pdf' | while read -r pdf; do
  cat "$pdf" > "$OUT/$(basename "$pdf")"
done

# Ship the fixture manifest too: litho's manifest.json declares Skips
# keyed on the PDF.js fixture ID (filename without extension), so
# callers can cross-reference.
cat "$TEST_DIR/test_manifest.json" > "/output/test_manifest.json" || true
