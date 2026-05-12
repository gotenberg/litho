#!/usr/bin/env bash
# gen-merge-empty.sh: produce one merge-with-empty bookmarks golden
# per PDF.
#
# For each PDF in the input directory (except empty.pdf itself):
#   1. pdfcpu merge out.pdf in.pdf empty.pdf
#   2. pdfcpu bookmarks export out.pdf out.json
#   3. save the JSON as <basename>.json under the output directory
#
# Merging each fixture with a known one-page empty.pdf exercises the
# real two-input merge path: the inputs bring genuinely different
# resource tables and outline trees, so the merge engine cannot
# shortcut through reference-identity deduplication the way merge-self
# can. The bookmark JSON captures pdfcpu's merge result so the
# merge-empty feature can cross-check litho's merge against it.
#
# Usage: gen-merge-empty.sh <pdfs_dir> <out_dir>
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
mkdir -p "$OUT_DIR"

EMPTY="$PDFS_DIR/pkg_testdata__empty.pdf"
if [ ! -f "$EMPTY" ]; then
  echo "ERROR: empty.pdf partner missing at $EMPTY" >&2
  exit 1
fi

if ! command -v pdfcpu > /dev/null; then
  echo "ERROR: pdfcpu CLI not on PATH" >&2
  exit 1
fi

total=0
ok=0
empty_outline=0
skipped=0
while IFS= read -r -d '' pdf; do
  # Skip empty.pdf itself: merging it with itself is degenerate and
  # already covered by the merge-self feature.
  if [ "$pdf" = "$EMPTY" ]; then
    continue
  fi
  total=$((total + 1))
  base=$(basename "$pdf" .pdf)
  out_pdf=$(mktemp /tmp/merge-empty.XXXXXX.pdf)
  out_json="$OUT_DIR/${base}.json"

  pages=$(pdfcpu info "$pdf" 2> /dev/null | awk '/Page count:/ {print $3; exit}')
  if [ -n "$pages" ] && [ "$pages" -gt 30 ]; then
    skipped=$((skipped + 1))
    rm -f "$out_pdf"
    continue
  fi

  if ! pdfcpu merge "$out_pdf" "$pdf" "$EMPTY" 2> /dev/null; then
    echo '{"bookmarks":[]}' > "$out_json"
    empty_outline=$((empty_outline + 1))
    rm -f "$out_pdf"
    continue
  fi

  if pdfcpu bookmarks export "$out_pdf" "$out_json" 2> /dev/null; then
    ok=$((ok + 1))
  else
    echo '{"bookmarks":[]}' > "$out_json"
    empty_outline=$((empty_outline + 1))
  fi
  rm -f "$out_pdf"
done < <(find "$PDFS_DIR" -maxdepth 1 -type f -name '*.pdf' -print0)

echo "  merge-empty: ${total} PDFs (${ok} with outline, ${empty_outline} empty, ${skipped} too large)"
