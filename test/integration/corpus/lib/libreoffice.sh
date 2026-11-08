#!/usr/bin/env bash
# lib/libreoffice.sh: build the LibreOffice reference PDFs of an Office corpus
# (libreoffice/<format> or gotenberg/<format>, for docx, odt, xlsx, ods, csv,
# pptx and odp).
#
# Starts the Gotenberg image versions.env pins (GOTENBERG_IMAGE) on a free
# loopback port and posts each source to its LibreOffice route,
# /forms/libreoffice/convert, writing the PDF to <out_dir>/<name>.pdf. The
# sources are the committed <corpus_dir>/sources/<name>/ directories, each
# one document (an extension of EXTENSIONS) and an optional options.json, and the
# LibreOffice test documents <corpus_dir>/documents.json lists, fetched into
# <fetched_dir> by libreoffice/fetch.sh, each named by its ID with "/" written
# "__" (ooxmlexport/tdf123.docx becomes ooxmlexport__tdf123.docx).
#
# The form fields come from the directory's options.json (FIXTURES.md, "Per
# fixture options"), each key the route's field of the same name; a key left
# out keeps the route's default: the document's own page setup, every page,
# the outline written, each sheet cut into pages, the hidden slides left out,
# no notes pages. The Go side reads the same file through
# fixture.LoadOffice (fixture/build and the diff feature of
# test/integration/cmd/corpus); keep the two in step.
#
# The references are not committed: they live in the gitignored testdata,
# with <out_dir>/manifest.json recording, per reference, the image, the pin
# the fetched documents come from (the corpus manifest's
# references.source_pin, LIBREOFFICE_CORE_COMMIT or POI_COMMIT for
# instance) and its value, and the SHA-256 of the source and of its
# options.json, so `corpus run` refuses a reference
# built from another pin or from a source that changed since. A source the
# route refuses is recorded under "failed" with the route's answer, and gets
# no reference. The optional names restrict the run to those references,
# keeping the manifest's other entries; a corpus holding none of them is left
# as it is. An environment variable of the same
# name overrides a pin.
#
# Usage: lib/libreoffice.sh <corpus_dir> <fetched_dir> <out_dir> [name...]
set -euo pipefail

CORPUS="${1:?usage: libreoffice.sh <corpus_dir> <fetched_dir> <out_dir> [name...]}"
FETCHED="${2:?usage: libreoffice.sh <corpus_dir> <fetched_dir> <out_dir> [name...]}"
OUT="${3:?usage: libreoffice.sh <corpus_dir> <fetched_dir> <out_dir> [name...]}"
shift 3
ONLY=("$@")
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"
NAME="$(basename "$(dirname "$CORPUS")")/$(basename "$CORPUS")"

pin() {
  local v="${!1:-}"
  [ -n "$v" ] || v="$(sed -n "s/^$1=//p" "$VERSIONS")"
  if [ -z "$v" ]; then
    echo "ERROR: $1 is not pinned in $VERSIONS" >&2
    exit 1
  fi
  printf '%s' "$v"
}
IMAGE="$(pin GOTENBERG_IMAGE)"
SOURCE_PIN="$(jq -r '.references.source_pin // empty' "$CORPUS/manifest.json")"
SOURCE_VALUE=""
[ -z "$SOURCE_PIN" ] || SOURCE_VALUE="$(pin "$SOURCE_PIN")"

for tool in docker curl jq shasum; do
  if ! command -v "$tool" > /dev/null; then
    echo "ERROR: $tool not on PATH" >&2
    exit 1
  fi
done

# The keys of options.json, the route's form fields (OfficeOptions in
# fixture/office.go). A key outside the list fails the build: a misspelt one
# would leave the fixture on the defaults.
KEYS='["password","landscape","nativePages","nativeBookmarks","paperWidth","paperHeight","singlePage","exportHiddenSlides","exportNotesPages"]'

# The extensions of a committed source's document (officeFormats in
# fixture/office.go).
EXTENSIONS='docx dotx odt fodt xlsx xlsm xltx xltm ods fods ots csv pptx pptm potx ppsx odp fodp otp doc xls ppt'

# is_document <file> reports whether the file's extension, in any case, is
# one of EXTENSIONS.
is_document() {
  local ext
  ext="$(printf '%s' "${1##*.}" | tr '[:upper:]' '[:lower:]')"
  [ "$ext" != "$1" ] && printf '%s\n' $EXTENSIONS | grep -qxF "$ext"
}

# selected <name> reports whether the run builds the reference name.
selected() {
  [ "${#ONLY[@]}" -eq 0 ] || printf '%s\n' "${ONLY[@]}" | grep -qxF "$1"
}

sha() {
  shasum -a 256 "$1" | cut -d ' ' -f 1
}

# The jobs, one "<name>\t<document>\t<options.json or ->\t<source path>"
# line each, the source path as the manifest records it: relative to the
# corpus directory for a committed source, to the fetched directory for a
# LibreOffice test document.
JOBS="$(mktemp)"
WORK="$(mktemp -d)"
trap 'rm -rf "$JOBS" "$WORK"' EXIT
for dir in "$CORPUS"/sources/*/; do
  [ -d "$dir" ] || continue
  dir="${dir%/}"
  name="$(basename "$dir")"
  selected "$name" || continue
  doc=""
  for f in "$dir"/*; do
    if [ "$(basename "$f")" = options.json ]; then
      continue
    fi
    if ! is_document "$(basename "$f")"; then
      echo "ERROR: $f is neither a document ($EXTENSIONS) nor options.json" >&2
      exit 1
    fi
    if [ -n "$doc" ]; then
      echo "ERROR: $dir holds more than one document" >&2
      exit 1
    fi
    doc="$f"
  done
  if [ -z "$doc" ]; then
    echo "ERROR: $dir holds no document ($EXTENSIONS)" >&2
    exit 1
  fi
  options=-
  if [ -f "$dir/options.json" ]; then
    options="$dir/options.json"
    if ! jq -e --argjson keys "$KEYS" 'type == "object" and ((keys - $keys) | length == 0)' "$options" > /dev/null; then
      echo "ERROR: $options is not an object of the keys $KEYS" >&2
      exit 1
    fi
  fi
  printf '%s\t%s\t%s\t%s\n' "$name" "$doc" "$options" "sources/$name/$(basename "$doc")" >> "$JOBS"
done
if [ -f "$CORPUS/documents.json" ]; then
  while IFS= read -r id; do
    name="${id//\//__}"
    selected "$name" || continue
    if [ ! -f "$FETCHED/$id" ]; then
      echo "ERROR: $FETCHED/$id is missing: run make office-fetch SOURCES=$(basename "$(dirname "$CORPUS")") FORMATS=$(basename "$CORPUS")" >&2
      exit 1
    fi
    printf '%s\t%s\t-\t%s\n' "$name" "$FETCHED/$id" "$id" >> "$JOBS"
  done < <(jq -r '.[].id' "$CORPUS/documents.json")
fi
if [ ! -s "$JOBS" ]; then
  if [ "${#ONLY[@]}" -gt 0 ]; then
    echo "  $NAME: none of the names is a source here"
    exit 0
  fi
  echo "ERROR: no source under $CORPUS matched" >&2
  exit 1
fi

VERSION="$(docker run --rm --entrypoint /usr/lib/libreoffice/program/soffice "$IMAGE" --headless --version 2> /dev/null | head -n 1 || true)"
CID="$(docker run -d --rm -p 127.0.0.1::3000 "$IMAGE")"
trap 'docker stop "$CID" > /dev/null 2>&1 || true; rm -rf "$JOBS" "$WORK"' EXIT
PORT="$(docker port "$CID" 3000/tcp | head -n 1 | sed 's/.*://')"
URL="http://127.0.0.1:$PORT"
for _ in $(seq 1 60); do
  if curl -fsS "$URL/health" > /dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "$URL/health" > /dev/null

# opt <options.json> <key>: the key's value as a form field value, or nothing
# when the file or the key is absent.
opt() {
  if [ -n "$1" ] && jq -e --arg k "$2" 'has($k)' "$1" > /dev/null; then
    jq -r --arg k "$2" '.[$k] | tostring' "$1"
  fi
}

# build_one <name> <document> <options> <source> posts one source and writes
# its reference, or its refusal, and the record of either in $WORK.
build_one() {
  local name="$1" doc="$2" options="$3" source="$4" key value status opts_sha=""
  local fields=()
  [ "$options" != - ] || options=""
  for key in $(jq -r '.[]' <<< "$KEYS"); do
    value="$(opt "$options" "$key")"
    [ -z "$value" ] || fields+=(--form-string "$key=$value")
  done
  [ -z "$options" ] || opts_sha="$(sha "$options")"
  status="$(curl -sS --retry 2 -o "$WORK/$name.out" -w '%{http_code}' \
    -F "files=@$doc" ${fields[@]+"${fields[@]}"} "$URL/forms/libreoffice/convert" || true)"
  [ -n "$status" ] || status=000
  if [ "$status" = 200 ] && [ "$(head -c 5 "$WORK/$name.out")" = "%PDF-" ]; then
    cat "$WORK/$name.out" > "$OUT/$name.pdf"
    jq -n --arg n "$name" --arg s "$source" --arg sh "$(sha "$doc")" --arg o "$opts_sha" \
      --arg p "$(sha "$OUT/$name.pdf")" --arg i "$IMAGE" --arg sp "$SOURCE_PIN" --arg sv "$SOURCE_VALUE" \
      '{($n): ({source: $s, source_sha256: $sh, options_sha256: $o, pdf_sha256: $p, image: $i} + (if $sp == "" then {} else {source_pin: $sp, source_pin_value: $sv} end))}' \
      > "$WORK/$name.ok"
    echo "  $NAME: references/$name.pdf ($(wc -c < "$OUT/$name.pdf" | tr -d ' ') bytes)"
  else
    rm -f "$OUT/$name.pdf"
    jq -n --arg n "$name" --arg s "$source" --arg sh "$(sha "$doc")" --arg o "$opts_sha" --arg i "$IMAGE" \
      --arg st "$status" --arg b "$(head -c 300 "$WORK/$name.out" 2> /dev/null | tr -d '\000' || true)" \
      '{($n): {source: $s, source_sha256: $sh, options_sha256: $o, image: $i, status: $st, answer: $b}}' \
      > "$WORK/$name.failed"
    echo "  $NAME: $name refused by the route (HTTP $status)" >&2
  fi
  rm -f "$WORK/$name.out"
}
export -f build_one opt sha
export OUT WORK URL IMAGE SOURCE_PIN SOURCE_VALUE CORPUS NAME KEYS

mkdir -p "$OUT"
# Four requests at a time: the route converts one document per LibreOffice
# instance and queues the rest, so more only waits longer.
tr '\t\n' '\0\0' < "$JOBS" | xargs -0 -n 4 -P 4 bash -c 'build_one "$0" "$1" "$2" "$3"'

MANIFEST="$OUT/manifest.json"
old='{}'
# A run of every source rewrites the manifest; a run of some keeps the
# entries of the others.
if [ "${#ONLY[@]}" -gt 0 ] && [ -s "$MANIFEST" ]; then
  old="$(cat "$MANIFEST")"
fi
ok="$(find "$WORK" -name '*.ok' -exec cat {} + | jq -s 'add // {}')"
failed="$(find "$WORK" -name '*.failed' -exec cat {} + | jq -s 'add // {}')"
built="$(jq -n --argjson ok "$ok" --argjson f "$failed" '($ok + $f) | keys')"
jq -n --argjson old "$old" --argjson ok "$ok" --argjson failed "$failed" --argjson built "$built" \
  --arg image "$IMAGE" --arg version "$VERSION" \
  '{format: 1, image: $image, libreoffice: $version,
    references: ((($old.references // {}) | with_entries(select(.key as $k | $built | index($k) | not))) + $ok),
    failed: ((($old.failed // {}) | with_entries(select(.key as $k | $built | index($k) | not))) + $failed)}' \
  > "$MANIFEST.part"
mv "$MANIFEST.part" "$MANIFEST"
echo "  $NAME: $(jq length <<< "$ok") references built, $(jq length <<< "$failed") refused, with ${IMAGE} (${VERSION})"
