#!/usr/bin/env bash
# lib/fetch-office.sh: download the LibreOffice test documents of an Office
# corpus (docx, odt, xlsx, ods, csv).
#
# <corpus_dir>/files.sha256 lists every document the corpus's documents.json
# names, one "<sha256>  <core path>" line each, paths relative to the root of
# LibreOffice core (sw/qa/extras/<dir>/data/<file>, sc/qa/unit/data/<ext>/<file>,
# chart2/qa/extras/data/<ext>/<file>), and the licence, COPYING.MPL. This
# script downloads each from raw.githubusercontent.com at the commit
# LIBREOFFICE_CORE_COMMIT pins, checks its SHA-256, and writes a document to
# <out_dir>/<dir>/<file>, the path documents.json keys it by (<dir> the
# Writer test directory, or sc or chart2), and the licence to
# <out_dir>/COPYING.MPL. A file already on disk with the listed
# hash is kept. A download that fails or hashes otherwise fails the run, so
# the tree on disk is always the pinned one.
#
# LibreOffice's test documents are under the Mozilla Public License 2.0 (some
# also under the LGPL 3.0 or later): they are downloaded for testing and never
# committed or redistributed.
#
# Usage: lib/fetch-office.sh <corpus_dir> <out_dir>
set -euo pipefail

CORPUS="${1:?usage: fetch-office.sh <corpus_dir> <out_dir>}"
OUT="${2:?usage: fetch-office.sh <corpus_dir> <out_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

COMMIT="${LIBREOFFICE_CORE_COMMIT:-$(sed -n 's/^LIBREOFFICE_CORE_COMMIT=//p' "$VERSIONS")}"
if [ -z "$COMMIT" ]; then
  echo "ERROR: LIBREOFFICE_CORE_COMMIT is not pinned in $VERSIONS" >&2
  exit 1
fi
BASE="https://raw.githubusercontent.com/LibreOffice/core/$COMMIT"

# dest <core_path> prints where a listed file is written.
dest() {
  case "$1" in
    sw/qa/extras/*/data/*)
      local rest="${1#sw/qa/extras/}"
      printf '%s/%s/%s' "$OUT" "${rest%%/*}" "${rest#*/data/}"
      ;;
    sc/qa/unit/data/*/* | chart2/qa/extras/data/*/*)
      printf '%s/%s/%s' "$OUT" "${1%%/*}" "$(basename "$1")"
      ;;
    *) printf '%s/%s' "$OUT" "$(basename "$1")" ;;
  esac
}

# fetch_one <sha256> <core_path> downloads one file unless it is already
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
export OUT BASE

mkdir -p "$OUT"
grep -v '^#' "$CORPUS/files.sha256" | awk 'NF == 2 { print $1 " " $2 }' \
  | xargs -P 8 -n 2 bash -c 'fetch_one "$0" "$1"'

count="$(grep -vc '^#' "$CORPUS/files.sha256")"
echo "  $(basename "$CORPUS"): ${count} files of LibreOffice core at ${COMMIT}, hash-checked"
