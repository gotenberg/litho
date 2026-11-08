#!/usr/bin/env bash
# lib/chromium.sh: build the Chromium reference PDFs of an html corpus
# (litho/html or gotenberg/html).
#
# Starts the Gotenberg image versions.env pins (GOTENBERG_IMAGE) on a free
# loopback port, posts each <corpus_dir>/sources/<name>/ directory to the
# Chromium route, and writes the PDF to <corpus_dir>/references/<name>.pdf. A directory holding index.html
# goes to /forms/chromium/convert/html with every sibling file; one holding
# index.md and no index.html goes to /forms/chromium/convert/markdown with an
# index.html that wraps it as litho's Markdown converter does (MarkdownWrapper
# in fixture/fixture.go).
#
# The form fields come from the directory's options.json (FIXTURES.md, "Per
# fixture options"), each key the route's field of the same name; a key left
# out keeps the corpus default, which every source without the file is built
# with: A4 (210 by 297 mm), the route's default margins (0.39 inch on every
# side, left unset), the print media type, backgrounds printed, since a
# document converter paints them, and the CSS page size preferred, since
# litho honoured @page size before the option existed. The files that header
# and footer name are posted as header.html and footer.html, the route's
# templates, and options.json is not posted. The Go side reads the same file
# through fixture.Load (fixture/htmlbuild and the diff feature of
# test/integration/cmd/corpus); keep the two in step.
#
# Chromium writes the time into each PDF, so two runs write other bytes; the
# PDFs are committed and rebuilt only when a source or the pin changes, which
# the optional names restrict the run to; a corpus holding none of them is
# left as it is. An environment variable of the same name overrides the pin.
#
# Usage: lib/chromium.sh <corpus_dir> [name...]
set -euo pipefail

CORPUS="${1:?usage: chromium.sh <corpus_dir> [name...]}"
shift
SOURCES="$CORPUS/sources"
OUT="$CORPUS/references"
NAME="$(basename "$(dirname "$CORPUS")")/$(basename "$CORPUS")"
ONLY=("$@")
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

IMAGE="${GOTENBERG_IMAGE:-$(sed -n 's/^GOTENBERG_IMAGE=//p' "$VERSIONS")}"
if [ -z "$IMAGE" ]; then
  echo "ERROR: GOTENBERG_IMAGE is not pinned in $VERSIONS" >&2
  exit 1
fi

for tool in docker curl jq; do
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

# The keys of options.json, the route's form fields (Options in
# test/integration/fixture/fixture.go). A key outside the list fails the build: a misspelt one
# would leave the fixture on the defaults.
KEYS='["paperWidth","paperHeight","marginTop","marginRight","marginBottom","marginLeft","landscape","scale","singlePage","nativePages","preferCssPageSize","printBackground","nativeBookmarks","emulatedMediaType","emulatedMediaFeatures","header","footer"]'

WRAP="$(mktemp -d)"
trap 'docker stop "$CID" > /dev/null 2>&1 || true; rm -rf "$WRAP"' EXIT

# opt <options.json> <key> <default>: the key's value as a form field value,
# or the default when the file or the key is absent. An object is written as
# compact JSON, as emulatedMediaFeatures takes it.
opt() {
  if [ -f "$1" ] && jq -e --arg k "$2" 'has($k)' "$1" > /dev/null; then
    jq -r --arg k "$2" '.[$k] | if type == "object" then tojson else tostring end' "$1"
  else
    printf '%s' "$3"
  fi
}

mkdir -p "$OUT"
count=0
for dir in "$SOURCES"/*/; do
  dir="${dir%/}"
  name="$(basename "$dir")"
  if [ "${#ONLY[@]}" -gt 0 ] && ! printf '%s\n' "${ONLY[@]}" | grep -qxF "$name"; then
    continue
  fi
  route=html
  if [ ! -f "$dir/index.html" ]; then
    [ -f "$dir/index.md" ] || continue
    route=markdown
  fi
  options="$dir/options.json"
  if [ -f "$options" ]; then
    if ! jq -e --argjson keys "$KEYS" 'type == "object" and ((keys - $keys) | length == 0)' "$options" > /dev/null; then
      echo "ERROR: $options is not an object of the keys $KEYS" >&2
      exit 1
    fi
  fi
  header="$(opt "$options" header "")"
  footer="$(opt "$options" footer "")"

  files=()
  while IFS= read -r -d '' f; do
    base="$(basename "$f")"
    case "$base" in
      options.json) continue ;;
    esac
    if [ "$base" = "$header" ] || [ "$base" = "$footer" ]; then
      continue
    fi
    files+=(-F "files=@$f")
  done < <(find "$dir" -maxdepth 1 \( -type f -o -type l \) -print0 | sort -z)
  if [ "$route" = markdown ]; then
    mkdir -p "$WRAP/$name"
    printf '%s' '<!doctype html><html><head><meta charset="utf-8"><title></title></head><body>{{ toHTML "index.md" }}</body></html>' > "$WRAP/$name/index.html"
    files+=(-F "files=@$WRAP/$name/index.html")
  fi
  [ -z "$header" ] || files+=(-F "files=@$dir/$header;filename=header.html")
  [ -z "$footer" ] || files+=(-F "files=@$dir/$footer;filename=footer.html")

  fields=(
    --form-string "paperWidth=$(opt "$options" paperWidth 210mm)"
    --form-string "paperHeight=$(opt "$options" paperHeight 297mm)"
    --form-string "emulatedMediaType=$(opt "$options" emulatedMediaType print)"
    --form-string "printBackground=$(opt "$options" printBackground true)"
    --form-string "preferCssPageSize=$(opt "$options" preferCssPageSize true)"
  )
  for key in marginTop marginRight marginBottom marginLeft landscape scale singlePage nativePages nativeBookmarks emulatedMediaFeatures; do
    value="$(opt "$options" "$key" "")"
    [ -z "$value" ] || fields+=(--form-string "$key=$value")
  done

  curl -fsS --retry 2 -o "$OUT/$name.pdf" \
    "${files[@]}" \
    "${fields[@]}" \
    "$URL/forms/chromium/convert/$route"
  echo "  $NAME: references/$name.pdf ($(wc -c < "$OUT/$name.pdf" | tr -d ' ') bytes, $route route)"
  count=$((count + 1))
done

if [ "$count" -eq 0 ] && [ "${#ONLY[@]}" -gt 0 ]; then
  echo "  $NAME: none of the names is a source here"
  exit 0
fi
if [ "$count" -eq 0 ]; then
  echo "ERROR: no sources/<name>/index.html or index.md under $SOURCES matched" >&2
  exit 1
fi
echo "  $NAME: ${count} Chromium references built with ${IMAGE}"
