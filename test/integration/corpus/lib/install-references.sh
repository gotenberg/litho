#!/usr/bin/env bash
# lib/install-references.sh: install the reference PDFs a converter corpus's
# host built into its testdata.
#
# The references sit in <output_pdfs_dir>/../references/, the testdata
# directory lib/libreoffice.sh writes, which the provisioning image mounts at
# /output. Each <name>.pdf is copied into <output_pdfs_dir>, where the diff
# goldens are generated from, and its SHA-256 checked against the
# references' manifest.json, so the goldens come from the references the
# corpus CLI checks. A corpus without references fails with the make target
# that builds them.
#
# Usage: lib/install-references.sh <output_pdfs_dir> <corpus>, the corpus
# named by its directory: libreoffice/docx, gotenberg/docx.
set -euo pipefail

OUT="$1"
CORPUS="$2"
BUILD="make office-references FORMATS=$(basename "$CORPUS")"
mkdir -p "$OUT"
SRC="$(cd "$OUT/.." && pwd)/references"
MANIFEST="$SRC/manifest.json"
if [ ! -s "$MANIFEST" ]; then
  echo "ERROR: no reference manifest at $MANIFEST: run $BUILD" >&2
  exit 1
fi

count=0
while IFS=$'\t' read -r name sha; do
  pdf="$SRC/$name.pdf"
  if [ ! -f "$pdf" ] || [ "$(sha256sum "$pdf" | cut -d ' ' -f 1)" != "$sha" ]; then
    echo "ERROR: $pdf is not the reference manifest.json records: run $BUILD" >&2
    exit 1
  fi
  # Plain shell redirect sidesteps cp/install's fallocate probe, which
  # breaks on macOS Docker Desktop's virtiofs bind mount.
  cat "$pdf" > "$OUT/$name.pdf"
  count=$((count + 1))
done < <(jq -r '.references | to_entries[] | "\(.key)\t\(.value.pdf_sha256)"' "$MANIFEST")

if [ "$count" -eq 0 ]; then
  echo "ERROR: no reference under $SRC: run $BUILD" >&2
  exit 1
fi
echo "  $CORPUS: ${count} LibreOffice references installed"
