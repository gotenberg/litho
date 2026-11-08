#!/usr/bin/env bash
# gotenberg/html/download.sh: install the Chromium reference PDFs of the gotenberg/html corpus.
#
# The corpus downloads nothing. Its references are Chromium's PDFs of its
# committed sources, which `make html-references` builds through the Chromium
# route of the Gotenberg image versions.env pins and commits in references/.
# This step is a plain copy from the corpus source directory the Dockerfile
# bakes in at /work/gotenberg/html; the provisioning image derives the diff goldens from
# the copies.
#
# Usage: gotenberg/html/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/references"
if [ ! -d "$SRC" ]; then
  echo "ERROR: reference directory not found at $SRC: run make html-references" >&2
  exit 1
fi

count=0
for pdf in "$SRC"/*.pdf; do
  [ -f "$pdf" ] || continue
  # Plain shell redirect sidesteps cp/install's fallocate probe, which
  # breaks on macOS Docker Desktop's virtiofs bind mount.
  cat "$pdf" > "$OUT/$(basename "$pdf")"
  count=$((count + 1))
done

if [ "$count" -eq 0 ]; then
  echo "ERROR: no Chromium reference under $SRC: run make html-references" >&2
  exit 1
fi
echo "  gotenberg/html: ${count} Chromium references installed"
