#!/usr/bin/env bash
# testdata/fetch-specs.sh: download the machine-readable standards the
# tests of this package read, at pinned versions, into testdata/specs/,
# which git ignores. TestUserAgentSheet and TestPropertyTableAgainstWebref
# skip when a file is absent.
#
#   - html-source: the HTML Standard's source at a pinned whatwg/html
#     commit (https://github.com/whatwg/html). Licence: Creative Commons
#     Attribution 4.0 International; portions incorporated into source
#     code under the BSD 3-Clause License (its LICENSE file).
#   - webref-css.json: css.json of the @webref/css package at a pinned
#     version (https://github.com/w3c/webref). Licence: MIT.
#
# Each file is checked against its pinned SHA-256 before it lands, and a
# mismatch fails the run. A directory that already records the pins is
# left alone.
#
# Usage: testdata/fetch-specs.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$HERE/specs"
HTML_COMMIT="${HTML_COMMIT:-c18a7c56c963e4b86c653b21f634fb49eb8ba44e}"
HTML_SHA256="3016f2e38edee0d04bb94a1bc2b8be2de07594c552e0c627131d6a6d9cd2a9d4"
WEBREF_VERSION="${WEBREF_VERSION:-8.7.8}"
WEBREF_SHA256="3121885b7cfa54f67354f7fcb68aa95b948e3b2b635f7134c5aab39cfa4f301f"
PINS="$HTML_COMMIT $WEBREF_VERSION"

# verify <file> <sha256> fails when file does not hash to sha256.
verify() {
  local got
  got="$(shasum -a 256 "$1" | cut -d ' ' -f 1)"
  if [ "$got" != "$2" ]; then
    echo "ERROR: $(basename "$1") hashes to $got, the pin is $2" >&2
    exit 1
  fi
}

if [ -f "$OUT/.pinned" ] && [ "$(cat "$OUT/.pinned")" = "$PINS" ]; then
  echo "  specs: already at $PINS"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL --retry 3 -o "$tmp/html-source" "https://raw.githubusercontent.com/whatwg/html/$HTML_COMMIT/source"
verify "$tmp/html-source" "$HTML_SHA256"
curl -fsSL --retry 3 -o "$tmp/webref.tgz" "https://registry.npmjs.org/@webref/css/-/css-$WEBREF_VERSION.tgz"
verify "$tmp/webref.tgz" "$WEBREF_SHA256"
tar -xzf "$tmp/webref.tgz" -C "$tmp" package/css.json
rm -rf "$OUT"
mkdir -p "$OUT"
cp "$tmp/html-source" "$OUT/html-source"
cp "$tmp/package/css.json" "$OUT/webref-css.json"
printf '%s' "$PINS" > "$OUT/.pinned"
echo "  specs: installed whatwg/html $HTML_COMMIT and @webref/css $WEBREF_VERSION"
