#!/usr/bin/env bash
# offcrypto/testdata/fetch.sh: download the encrypted Office samples the
# offcrypto tests open.
#
# files.sha256 lists each sample with its SHA-256, its source repository and
# the commit it is pinned at. This script downloads each from
# raw.githubusercontent.com, checks its hash, and writes it under
# <out_dir>/<source>/<path>. A file already on disk with the listed hash is
# kept; a download that hashes otherwise fails the run. Nothing it
# downloads is run. The samples are never committed.
#
# Usage: fetch.sh [out_dir], out_dir defaulting to
# test/integration/testdata/offcrypto.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${1:-$HERE/../../../../test/integration/testdata/offcrypto}"

repo() {
  case "$1" in
    msoffcrypto) echo nolze/msoffcrypto-tool ;;
    gotenberg) echo gotenberg/gotenberg ;;
    *)
      echo "ERROR: unknown source $1" >&2
      return 1
      ;;
  esac
}

grep -v '^#' "$HERE/files.sha256" | while read -r sha src commit path; do
  [ -n "$sha" ] || continue
  dst="$OUT/$src/$path"
  if [ -f "$dst" ] && [ "$(shasum -a 256 "$dst" | cut -d ' ' -f 1)" = "$sha" ]; then
    continue
  fi
  mkdir -p "$(dirname "$dst")"
  curl -fsSL --retry 3 -o "$dst.part" "https://raw.githubusercontent.com/$(repo "$src")/$commit/$path"
  got="$(shasum -a 256 "$dst.part" | cut -d ' ' -f 1)"
  if [ "$got" != "$sha" ]; then
    rm -f "$dst.part"
    echo "ERROR: $src/$path hashes to $got, files.sha256 lists $sha" >&2
    exit 1
  fi
  mv "$dst.part" "$dst"
done
echo "  offcrypto: $(grep -vc '^#' "$HERE/files.sha256") samples in $OUT, hash-checked"
