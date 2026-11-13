#!/usr/bin/env bash
# testdata/fetch-specs.sh: download the machine-readable standards the
# extracts of testdata/specs/ come from, at pinned versions, into
# testdata/specs/download/, which git ignores.
#
# What git holds and what it does not:
#
#   - testdata/specs/html-rendering.tsv (committed): the declarations of
#     the CSS of the HTML Standard's Rendering section, which
#     TestUserAgentSheet holds ua.css to.
#   - testdata/specs/webref-properties.json (committed): the name,
#     initial value, inheritance and legacy alias target of every property
#     of @webref/css, which TestPropertyTableAgainstWebref holds the
#     property table to.
#   - testdata/specs/download/ (ignored): the downloads below. Only
#     TestSpecExtractsMatchDownloads reads them, and it skips without them.
#
# The downloads:
#
#   - html-source and html-LICENSE: the HTML Standard's source and its
#     LICENSE file at a pinned whatwg/html commit
#     (https://github.com/whatwg/html). Licence: Creative Commons
#     Attribution 4.0 International; portions incorporated into source
#     code under the BSD 3-Clause License.
#   - webref-css.tgz: the @webref/css package at a pinned version
#     (https://github.com/w3c/webref), whose package.json declares the MIT
#     licence. The package carries no copyright or permission notice:
#     webref-LICENSE is the LICENSE file of the w3c/webref repository at a
#     pinned commit.
#
# Each file is checked against its pinned SHA-256 before it lands, and a
# mismatch fails the run. A directory that already records the pins is
# left alone.
#
# To move a pin: change the commit or version and its SHA-256 below, run
# this script, then rewrite the extracts from the new downloads and review
# their diff, ua-differences.tsv and webref-differences.tsv with them:
#
#   pkg/internal/css/testdata/fetch-specs.sh
#   go test ./pkg/internal/css -run TestSpecExtractsMatchDownloads -update-css-specs
#   go test ./pkg/internal/css
#
# Usage: testdata/fetch-specs.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$HERE/specs/download"
HTML_COMMIT="${HTML_COMMIT:-c18a7c56c963e4b86c653b21f634fb49eb8ba44e}"
HTML_SHA256="3016f2e38edee0d04bb94a1bc2b8be2de07594c552e0c627131d6a6d9cd2a9d4"
HTML_LICENSE_SHA256="85dc6f5ccb57a6fe8c33d158f9fc8fc7ee5655a5d3db2cdd131c6a3d0f48a864"
WEBREF_VERSION="${WEBREF_VERSION:-8.7.8}"
WEBREF_SHA256="3121885b7cfa54f67354f7fcb68aa95b948e3b2b635f7134c5aab39cfa4f301f"
WEBREF_LICENSE_COMMIT="9cab3acb0744011311d9f4b910c675cd46de5edd"
WEBREF_LICENSE_SHA256="0c858684c0cd758f3bc5198c1f5d5684aecbece0713a0e9b17d740ba5bf8db1a"
PINS="$HTML_COMMIT $WEBREF_VERSION $WEBREF_LICENSE_COMMIT"

# fetch <url> <file> <sha256> downloads url into file and fails when it
# does not hash to sha256.
fetch() {
  local got
  curl -fsSL --retry 3 -o "$2" "$1"
  got="$(shasum -a 256 "$2" | cut -d ' ' -f 1)"
  if [ "$got" != "$3" ]; then
    echo "ERROR: $(basename "$2") hashes to $got, the pin is $3" >&2
    exit 1
  fi
}

if [ -f "$OUT/.pinned" ] && [ "$(cat "$OUT/.pinned")" = "$PINS" ]; then
  echo "  specs: already at $PINS"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fetch "https://raw.githubusercontent.com/whatwg/html/$HTML_COMMIT/source" "$tmp/html-source" "$HTML_SHA256"
fetch "https://raw.githubusercontent.com/whatwg/html/$HTML_COMMIT/LICENSE" "$tmp/html-LICENSE" "$HTML_LICENSE_SHA256"
fetch "https://registry.npmjs.org/@webref/css/-/css-$WEBREF_VERSION.tgz" "$tmp/webref-css.tgz" "$WEBREF_SHA256"
fetch "https://raw.githubusercontent.com/w3c/webref/$WEBREF_LICENSE_COMMIT/LICENSE" "$tmp/webref-LICENSE" "$WEBREF_LICENSE_SHA256"
rm -rf "$OUT"
mkdir -p "$OUT"
cp "$tmp/html-source" "$tmp/html-LICENSE" "$tmp/webref-css.tgz" "$tmp/webref-LICENSE" "$OUT/"
printf '%s' "$PINS" > "$OUT/.pinned"
echo "  specs: installed whatwg/html $HTML_COMMIT, @webref/css $WEBREF_VERSION and w3c/webref's LICENSE at $WEBREF_LICENSE_COMMIT"
