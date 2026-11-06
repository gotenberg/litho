#!/usr/bin/env bash
# svg/fetch.sh: download the resvg tests the svg corpus scores.
#
# files.sha256 lists every file the tests in tests.json read: each SVG, its
# reference PNG, the Noto fonts the text tests draw with and their licence,
# and resvg's MIT licence, one "<sha256>  <path>" line each, paths relative
# to the resvg repository root. This script downloads each from
# raw.githubusercontent.com at the commit RESVG_COMMIT pins, checks its
# SHA-256, and writes it under <svg_dir> at its path below
# crates/resvg/tests/tests/; the fonts go to <fonts_dir>/resvg instead,
# beside the other fixture fonts. A file already on disk with the listed
# hash is kept. A download that fails or hashes otherwise fails the run, so
# the tree on disk is always the pinned one. Nothing it downloads is run.
# The script then records the commit in <svg_dir>/versions.json, which the
# corpus CLI checks before it scores.
#
# Usage: svg/fetch.sh <svg_dir> <fonts_dir>
set -euo pipefail

OUT="${1:?usage: fetch.sh <svg_dir> <fonts_dir>}"
FONTS="${2:?usage: fetch.sh <svg_dir> <fonts_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

COMMIT="${RESVG_COMMIT:-$(sed -n 's/^RESVG_COMMIT=//p' "$VERSIONS")}"
if [ -z "$COMMIT" ]; then
  echo "ERROR: RESVG_COMMIT is not pinned in $VERSIONS" >&2
  exit 1
fi
BASE="https://raw.githubusercontent.com/linebender/resvg/$COMMIT"

# dest <resvg_path> prints where a listed file is written.
dest() {
  case "$1" in
    crates/resvg/tests/tests/*) printf '%s/%s' "$OUT" "${1#crates/resvg/tests/tests/}" ;;
    crates/resvg/tests/fonts/*) printf '%s/resvg/%s' "$FONTS" "${1#crates/resvg/tests/fonts/}" ;;
    *) printf '%s/%s' "$OUT" "$1" ;;
  esac
}

# fetch_one <sha256> <resvg_path> downloads one file unless it is already
# there with the listed hash, and fails when the download hashes otherwise.
fetch_one() {
  local sha="$1" rel="$2" dst tmp got
  dst="$(dest "$rel")"
  if [ -f "$dst" ] && [ "$(shasum -a 256 "$dst" | cut -d ' ' -f 1)" = "$sha" ]; then
    return 0
  fi
  mkdir -p "$(dirname "$dst")"
  tmp="$dst.part"
  curl -fsSL --retry 3 -o "$tmp" "$BASE/$rel"
  got="$(shasum -a 256 "$tmp" | cut -d ' ' -f 1)"
  if [ "$got" != "$sha" ]; then
    rm -f "$tmp"
    echo "ERROR: $rel hashes to $got, files.sha256 lists $sha" >&2
    return 1
  fi
  mv "$tmp" "$dst"
}
export -f fetch_one dest
export OUT FONTS BASE

mkdir -p "$OUT" "$FONTS"
# A version record from another commit names files this run may not replace:
# drop it first, so an interrupted run leaves no record at all.
rm -f "$OUT/versions.json"
grep -v '^#' "$HERE/files.sha256" | awk 'NF == 2 { print $1 " " $2 }' \
  | xargs -P 8 -n 2 bash -c 'fetch_one "$0" "$1"'

count="$(grep -vc '^#' "$HERE/files.sha256")"
printf '{"format": 1, "stages": {"pdfs": {"recorded_at": "%s", "pins": {"RESVG_COMMIT": "%s"}}}}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$COMMIT" > "$OUT/versions.json"
echo "  svg: ${count} files at ${COMMIT}, hash-checked"
