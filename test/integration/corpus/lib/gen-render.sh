#!/usr/bin/env bash
# gen-render.sh: render goldens (poppler pdftoppm).
#
# Renders the first RENDER_MAX_PAGES pages of each sampled PDF at
# RENDER_DPI into <out_dir>/<id>.<page>.png, and writes <out_dir>/<id>.json
# naming the pages it rendered, the page count, and every failure, so the
# render feature tells a page the oracle could not render from one it never
# tried. The render feature in test/integration/cmd/corpus mirrors both
# constants (renderDPI, renderMaxPages): keep the three in step.
#
# Settings, each passed explicitly so a poppler default that moves cannot move
# the goldens:
#   - 72 dpi: one pixel per default user space unit (ISO 32000-1, 8.3.2.3),
#     so a page's user space maps to device space by an integer scale. A US
#     Letter page is 612x792 pixels, small enough to keep the goldens of a
#     whole corpus in a few tens of megabytes, while a misplaced path, a wrong
#     colour or a missing glyph still covers pixels.
#   - -cropbox: the region a viewer displays (ISO 32000-1, 14.11.2), not the
#     media box pdftoppm renders by default.
#   - -aa yes -aaVector yes: anti-aliased text and vector graphics, as viewers
#     draw them.
#   - Annotations are drawn, as viewers draw them.
#
# A PDF is rendered when the first two hex digits of the SHA-256 of its file
# name, read as a number, are below <sample_per_256>: 256 renders every PDF,
# 16 one in sixteen. Hashing the name keeps the sample stable when the corpus
# gains or loses other files, and independent of the order files are listed
# in.
#
# Usage: gen-render.sh <pdfs_dir> <out_dir> <sample_per_256>
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
SAMPLE="${3:-256}"
mkdir -p "$OUT_DIR"

RENDER_DPI=72
RENDER_MAX_PAGES=3
# RENDER_TIMEOUT bounds one pdftoppm or pdfinfo call, in seconds. A crafted
# PDF can keep poppler busy indefinitely; at 72 dpi a page takes well under a
# second.
RENDER_TIMEOUT=120

for tool in pdftoppm pdfinfo jq sha256sum timeout; do
  if ! command -v "$tool" > /dev/null; then
    echo "ERROR: $tool not on PATH" >&2
    exit 1
  fi
done

# render_one renders one PDF and writes its index. Every failure is recorded
# in the index rather than ending the run.
render_one() {
  local pdf="$1" id hash pages err pages_json='[]' page last out
  id=$(basename "$pdf" .pdf)
  hash=$(printf '%s' "$id.pdf" | sha256sum | cut -c1-2)
  if [ $((16#$hash)) -ge "$SAMPLE" ]; then
    return 0
  fi
  if ! err=$(timeout "$RENDER_TIMEOUT" pdfinfo "$pdf" 2>&1 > "$OUT_DIR/$id.info"); then
    jq -n --arg id "$id" --argjson dpi "$RENDER_DPI" --arg error "pdfinfo: ${err:-exit status}" \
      '{id: $id, dpi: $dpi, page_count: 0, pages: [], error: $error}' > "$OUT_DIR/$id.json"
    rm -f "$OUT_DIR/$id.info"
    return 0
  fi
  pages=$(awk '/^Pages:/ { print $2 }' "$OUT_DIR/$id.info")
  rm -f "$OUT_DIR/$id.info"
  pages=${pages:-0}
  last=$((pages < RENDER_MAX_PAGES ? pages : RENDER_MAX_PAGES))
  for ((page = 1; page <= last; page++)); do
    out="$OUT_DIR/$id.$page"
    if err=$(timeout "$RENDER_TIMEOUT" pdftoppm -r "$RENDER_DPI" -cropbox -aa yes -aaVector yes \
      -f "$page" -l "$page" -singlefile -png "$pdf" "$out" 2>&1) && [ -s "$out.png" ]; then
      pages_json=$(jq -c --argjson page "$page" --arg file "$id.$page.png" \
        '. + [{page: $page, file: $file}]' <<< "$pages_json")
    else
      rm -f "$out.png"
      pages_json=$(jq -c --argjson page "$page" --arg error "pdftoppm: ${err:-no output}" \
        '. + [{page: $page, error: $error}]' <<< "$pages_json")
    fi
  done
  jq -n --arg id "$id" --argjson dpi "$RENDER_DPI" --argjson count "$pages" --argjson pages "$pages_json" \
    '{id: $id, dpi: $dpi, page_count: $count, pages: $pages}' > "$OUT_DIR/$id.json"
}
export -f render_one
export OUT_DIR SAMPLE RENDER_DPI RENDER_MAX_PAGES RENDER_TIMEOUT

find "$PDFS_DIR" -maxdepth 1 -type f -name '*.pdf' -print0 \
  | sort -z \
  | parallel -0 -j "$(nproc)" render_one {}

indexed=$(find "$OUT_DIR" -maxdepth 1 -name '*.json' | wc -l)
rendered=$(find "$OUT_DIR" -maxdepth 1 -name '*.png' | wc -l)
failed=$(cat "$OUT_DIR"/*.json 2> /dev/null | jq -s '[.[] | (if .error then 1 else 0 end) + ([.pages[] | select(.error)] | length)] | add // 0')
echo "  render: ${indexed} PDFs sampled, ${rendered} pages rendered, ${failed} failures"
if [ "$indexed" -eq 0 ]; then
  echo "ERROR: the sample selected no PDF" >&2
  exit 1
fi
