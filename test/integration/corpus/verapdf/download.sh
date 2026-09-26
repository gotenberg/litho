#!/usr/bin/env bash
# verapdf/download.sh: fetch the VeraPDF corpus at the pinned revision
# (VERAPDF_CORPUS_REVISION, from versions.env) and flatten its
# profile-based tree into a single pdfs/ directory. Each PDF gets a
# prefix from its source profile directory so basenames stay unique.
#
# Usage: verapdf/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"

/usr/local/bin/fetch-revision https://github.com/veraPDF/veraPDF-corpus.git \
  "${VERAPDF_CORPUS_REVISION:?}" /tmp/verapdf-corpus

for dir in /tmp/verapdf-corpus/PDF_A-* /tmp/verapdf-corpus/PDF_UA-*; do
  [ -d "$dir" ] || continue
  prefix=$(basename "$dir")
  find "$dir" -type f -name '*.pdf' | while read -r pdf; do
    base=$(basename "$pdf")
    # Some VeraPDF fixtures use spaces in paths. Collapse to underscores
    # inside the flat layout to keep fixture IDs shell-friendly.
    safe_base=$(echo "$base" | tr ' ' '_')
    # Plain shell redirect sidesteps cp/install's fallocate probe,
    # which fails on macOS Docker Desktop's virtiofs bind mount.
    cat "$pdf" > "$OUT/${prefix}__${safe_base}"
  done
done
