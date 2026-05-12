#!/usr/bin/env bash
# gen-bookmarks.sh: produce one bookmarks golden JSON per PDF.
#
# Invokes `pdfcpu bookmarks export` against every PDF in the input
# directory and writes the result as <basename>.json under the output
# directory. PDFs without an outline (the majority of the corpus) make
# pdfcpu emit an error; that case is captured as the canonical empty
# golden ('{"bookmarks":[]}') so the bookmarks feature can still pair
# them against litho's empty extraction result.
#
# Usage: gen-bookmarks.sh <pdfs_dir> <out_dir>
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
mkdir -p "$OUT_DIR"

if ! command -v pdfcpu > /dev/null; then
  echo "ERROR: pdfcpu CLI not on PATH" >&2
  exit 1
fi

total=0
found=0
empty=0
failed=0
while IFS= read -r -d '' pdf; do
  total=$((total + 1))
  base=$(basename "$pdf" .pdf)
  out="$OUT_DIR/${base}.json"
  if pdfcpu bookmarks export "$pdf" "$out" 2> /dev/null; then
    found=$((found + 1))
  else
    # pdfcpu errors on PDFs with no outline. Capture an empty golden
    # so the runner pairs them against litho's empty result instead
    # of dropping them from the denominator.
    echo '{"bookmarks":[]}' > "$out"
    empty=$((empty + 1))
  fi
  # Treat unreadable PDFs as 'failed' only when even the empty fallback
  # cannot be written. pdfcpu's exit code itself is not a reliable
  # signal because it returns non-zero for any PDF without bookmarks.
  if [ ! -s "$out" ]; then
    failed=$((failed + 1))
  fi
done < <(find "$PDFS_DIR" -maxdepth 1 -type f -name '*.pdf' -print0)

echo "  bookmarks: ${total} PDFs (${found} with outline, ${empty} empty, ${failed} failed)"
