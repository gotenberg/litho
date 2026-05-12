#!/usr/bin/env bash
# gen-merges.sh: produce one merge-bookmarks golden per PDF.
#
# For each PDF in the input directory:
#   1. Run pdfcpu merge in.pdf in.pdf out.pdf (merge-self scenario)
#   2. Run pdfcpu bookmarks export out.pdf out.json
#   3. Save the JSON as <basename>.json under the output directory
#
# Self-merge gives the feature one fixture per PDF that fits the
# matrix's per-PDF iteration. The bookmark JSON captures pdfcpu's
# wrapped-outline output so the merge feature can cross-check
# litho's merge against it.
#
# Usage: gen-merges.sh <pdfs_dir> <out_dir>
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
mkdir -p "$OUT_DIR"

if ! command -v pdfcpu > /dev/null; then
  echo "ERROR: pdfcpu CLI not on PATH" >&2
  exit 1
fi

total=0
ok=0
empty=0
skipped=0
while IFS= read -r -d '' pdf; do
  total=$((total + 1))
  base=$(basename "$pdf" .pdf)
  out_pdf=$(mktemp /tmp/merge-out.XXXXXX.pdf)
  out_json="$OUT_DIR/${base}.json"

  # Skip large PDFs: merging a 1000-page document with itself is 2000
  # pages of writing per fixture and the feature will skip them at
  # runtime anyway. The threshold matches the runtime cap.
  pages=$(pdfcpu info "$pdf" 2> /dev/null | awk '/Page count:/ {print $3; exit}')
  if [ -n "$pages" ] && [ "$pages" -gt 30 ]; then
    skipped=$((skipped + 1))
    rm -f "$out_pdf"
    continue
  fi

  if ! pdfcpu merge "$out_pdf" "$pdf" "$pdf" 2> /dev/null; then
    # Merge failed: emit empty golden so the runner pairs it as
    # skipped (no bookmarks to validate) rather than dropping the
    # fixture from the denominator.
    echo '{"bookmarks":[]}' > "$out_json"
    empty=$((empty + 1))
    rm -f "$out_pdf"
    continue
  fi

  if pdfcpu bookmarks export "$out_pdf" "$out_json" 2> /dev/null; then
    ok=$((ok + 1))
  else
    # No outline in the merged result (input had none).
    echo '{"bookmarks":[]}' > "$out_json"
    empty=$((empty + 1))
  fi
  rm -f "$out_pdf"
done < <(find "$PDFS_DIR" -maxdepth 1 -type f -name '*.pdf' -print0)

echo "  merges: ${total} PDFs (${ok} with outline, ${empty} empty, ${skipped} too large)"
