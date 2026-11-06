#!/usr/bin/env bash
# html/chromium.sh: build the Chromium reference PDFs of the html corpus.
#
# Starts the Gotenberg image versions.env pins (GOTENBERG_IMAGE) on a free
# loopback port, posts each sources/<name>/ directory to its
# /forms/chromium/convert/html route, index.html and every sibling file, and
# writes the PDF to <out_dir>/<name>.pdf. The form fields are the ones the
# litho side converts with (build/main.go and the diff feature of
# test/integration/cmd/corpus): A4 (210 by 297 mm), the route's default
# margins (0.39 inch on every side, left unset), the print media type,
# backgrounds printed, since a document converter paints them, and the CSS
# page size preferred, since litho honours @page size (the route's default
# of ignoring it is a Gotenberg option a later wave adds). Keep the three in
# step.
#
# Chromium writes the time into each PDF, so two runs write other bytes; the
# PDFs are committed and rebuilt only when a source or the pin changes. An
# environment variable of the same name overrides the pin.
#
# Usage: html/chromium.sh <sources_dir> <out_dir>
set -euo pipefail

SOURCES="${1:?usage: chromium.sh <sources_dir> <out_dir>}"
OUT="${2:?usage: chromium.sh <sources_dir> <out_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

IMAGE="${GOTENBERG_IMAGE:-$(sed -n 's/^GOTENBERG_IMAGE=//p' "$VERSIONS")}"
if [ -z "$IMAGE" ]; then
  echo "ERROR: GOTENBERG_IMAGE is not pinned in $VERSIONS" >&2
  exit 1
fi

for tool in docker curl; do
  if ! command -v "$tool" > /dev/null; then
    echo "ERROR: $tool not on PATH" >&2
    exit 1
  fi
done

CID="$(docker run -d --rm -p 127.0.0.1::3000 "$IMAGE")"
trap 'docker stop "$CID" > /dev/null 2>&1 || true' EXIT
PORT="$(docker port "$CID" 3000/tcp | head -n 1 | sed 's/.*://')"
URL="http://127.0.0.1:$PORT"

# Chromium starts lazily; the health route answers once the modules are up.
for _ in $(seq 1 60); do
  if curl -fsS "$URL/health" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "$URL/health" > /dev/null

mkdir -p "$OUT"
count=0
for dir in "$SOURCES"/*/; do
  name="$(basename "$dir")"
  [ -f "$dir/index.html" ] || continue
  files=()
  while IFS= read -r -d '' f; do
    files+=(-F "files=@$f")
  done < <(find "$dir" -maxdepth 1 -type f -print0 | sort -z)
  curl -fsS --retry 2 -o "$OUT/$name.pdf" \
    "${files[@]}" \
    -F paperWidth=210mm \
    -F paperHeight=297mm \
    -F emulatedMediaType=print \
    -F printBackground=true \
    -F preferCssPageSize=true \
    "$URL/forms/chromium/convert/html"
  echo "  html: chromium/$name.pdf ($(wc -c < "$OUT/$name.pdf" | tr -d ' ') bytes)"
  count=$((count + 1))
done

if [ "$count" -eq 0 ]; then
  echo "ERROR: no sources/<name>/index.html under $SOURCES" >&2
  exit 1
fi
echo "  html: ${count} Chromium references built with ${IMAGE}"
