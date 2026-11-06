#!/usr/bin/env bash
# wpt/fetch.sh: download the web-platform-tests files the wpt corpus scores.
#
# files.sha256 lists every file the reftests in reftests.json read: each test,
# each reference, the stylesheets and images they name, the WPT licence and
# fonts/Ahem.ttf, one "<sha256>  <path>" line each, paths relative to the WPT
# root. This script downloads each from raw.githubusercontent.com at the
# commit WPT_COMMIT pins, checks its SHA-256, and writes it under <wpt_dir> at
# its WPT path; Ahem goes to <fonts_dir>/ahem instead, beside the other fixture
# fonts. A file already on disk with the listed hash is kept. A download that
# fails or hashes otherwise fails the run, so the tree on disk is always the
# pinned one. The script then records the commit in <wpt_dir>/versions.json,
# which the corpus CLI checks before it scores.
#
# Usage: wpt/fetch.sh <wpt_dir> <fonts_dir>
set -euo pipefail

OUT="${1:?usage: fetch.sh <wpt_dir> <fonts_dir>}"
FONTS="${2:?usage: fetch.sh <wpt_dir> <fonts_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

COMMIT="${WPT_COMMIT:-$(sed -n 's/^WPT_COMMIT=//p' "$VERSIONS")}"
if [ -z "$COMMIT" ]; then
  echo "ERROR: WPT_COMMIT is not pinned in $VERSIONS" >&2
  exit 1
fi
BASE="https://raw.githubusercontent.com/web-platform-tests/wpt/$COMMIT"

# dest <wpt_path> prints where a listed file is written.
dest() {
  case "$1" in
    fonts/Ahem.ttf) printf '%s/ahem/Ahem.ttf' "$FONTS" ;;
    *) printf '%s/%s' "$OUT" "$1" ;;
  esac
}

# fetch_one <sha256> <wpt_path> downloads one file unless it is already
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
printf '{"format": 1, "stages": {"pdfs": {"recorded_at": "%s", "pins": {"WPT_COMMIT": "%s"}}}}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$COMMIT" > "$OUT/versions.json"
echo "  wpt: ${count} files at ${COMMIT}, hash-checked"
