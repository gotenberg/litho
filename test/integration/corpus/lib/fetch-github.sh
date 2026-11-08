#!/usr/bin/env bash
# lib/fetch-github.sh: download the documents of an Office corpus from a
# GitHub repository at a pinned commit.
#
# <corpus_dir>/documents.json lists each document as {"id", "path"}, the
# path its path in the repository, and <corpus_dir>/files.sha256 lists
# "<sha256>  <path>" for each, then the licence files. This script downloads
# each document from raw.githubusercontent.com at the commit the pin names
# into <out_dir>/<id>, and each licence file into <out_dir>/<file name>,
# refusing one whose SHA-256 is not the listed one. A file already on disk
# with the listed hash is kept, so the tree on disk is always the pinned one.
# An environment variable of the pin's name overrides versions.env.
#
# Usage: lib/fetch-github.sh <owner/repo> <pin_name> <corpus_dir> <out_dir>
set -euo pipefail

REPO="${1:?usage: fetch-github.sh <owner/repo> <pin_name> <corpus_dir> <out_dir>}"
PIN="${2:?usage: fetch-github.sh <owner/repo> <pin_name> <corpus_dir> <out_dir>}"
CORPUS="${3:?usage: fetch-github.sh <owner/repo> <pin_name> <corpus_dir> <out_dir>}"
OUT="${4:?usage: fetch-github.sh <owner/repo> <pin_name> <corpus_dir> <out_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

COMMIT="${!PIN:-$(sed -n "s/^$PIN=//p" "$VERSIONS")}"
if [ -z "$COMMIT" ]; then
  echo "ERROR: $PIN is not pinned in $VERSIONS" >&2
  exit 1
fi
BASE="https://raw.githubusercontent.com/$REPO/$COMMIT"

# fetch_one <sha256> <path> <dest> downloads one file unless it is already
# there with the listed hash; the path is URL-encoded segment by segment,
# since some names hold spaces.
fetch_one() {
  local sha="$1" rel="$2" dst="$3" url tmp got
  if [ -f "$dst" ] && [ "$(shasum -a 256 "$dst" | cut -d ' ' -f 1)" = "$sha" ]; then
    return 0
  fi
  url="$BASE/$(jq -rn --arg p "$rel" '$p | split("/") | map(@uri) | join("/")')"
  mkdir -p "$(dirname "$dst")"
  tmp="$dst.part"
  curl -fsSL --retry 3 -o "$tmp" "$url"
  got="$(shasum -a 256 "$tmp" | cut -d ' ' -f 1)"
  if [ "$got" != "$sha" ]; then
    rm -f "$tmp"
    echo "ERROR: $rel hashes to $got, files.sha256 lists $sha" >&2
    return 1
  fi
  mv "$tmp" "$dst"
}
export -f fetch_one
export BASE

mkdir -p "$OUT"
# One line per file: sha256, path, destination, NUL-separated for xargs.
jq -r --rawfile sums "$CORPUS/files.sha256" --arg out "$OUT" '
  ($sums | split("\n") | map(select(length > 0) | capture("^(?<h>[0-9a-f]{64})  (?<p>.*)$")) | map({(.p): .h}) | add) as $hash
  | (map({(.path): .id}) | add) as $ids
  | $hash | to_entries[]
  | [.value, .key, ($out + "/" + ($ids[.key] // (.key | split("/") | last)))]
  | join("\u0000")' "$CORPUS/documents.json" \
  | tr '\n' '\0' | xargs -0 -n 3 -P 8 bash -c 'fetch_one "$0" "$1" "$2"'
echo "  $(basename "$(dirname "$CORPUS")")/$(basename "$CORPUS"): $(grep -c . "$CORPUS/files.sha256") files of $REPO at ${COMMIT}, hash-checked"
