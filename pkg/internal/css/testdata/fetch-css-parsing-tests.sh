#!/usr/bin/env bash
# testdata/fetch-css-parsing-tests.sh: download the css-parsing-tests suite
# (https://github.com/SimonSapin/css-parsing-tests, the suite tinycss2 and
# rust-cssparser run) at a pinned commit into testdata/css-parsing-tests/,
# which git ignores. TestCSSParsingTests skips when the directory is absent.
#
# Each file is checked against its pinned SHA-256 before it lands, and a
# mismatch fails the run. A directory that already records the pinned
# commit is left alone.
#
# Usage: testdata/fetch-css-parsing-tests.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$HERE/css-parsing-tests"
COMMIT="${CSS_PARSING_TESTS_COMMIT:-203ce36bffd617db7f118c551e32794561fb273d}"
BASE="https://raw.githubusercontent.com/SimonSapin/css-parsing-tests/$COMMIT"

# FILES pairs each file with its SHA-256 at COMMIT.
FILES=(
  "An+B.json 0deb798e84ecf7f08de3c89b3ecdc65caceb8f31a45b03fcf3622e6ba69dfd2b"
  "color_hexadecimal_3.json 0ed46e6f0b465aa50917dcefbafd09bcb89c9f1746f86fba360ee79141bf8e23"
  "color_hsl_3.json f3967564ee5fe5903126e415240d4b2b5ba90e760e29c3760bbb076a9af4f78b"
  "color_keywords_3.json b3a92cdfc563e2bc5cfa5118bd0591672b054eb624a2a30ad27bf1e5e5f27e0d"
  "component_value_list.json a8d7a5252373b892cfcac359360930ad9a57ed918a84331bcf0c872b80f83200"
  "declaration_list.json 5d9e4680f64e9a92d9e668d1bc0a69168ab9bb8c6d392de8222b90e9dd88f052"
  "one_component_value.json 34b568c084c48b26c13e991e297be6d71ce3793395335f43f85d665e0c15cac9"
  "one_declaration.json 5360083bfba780c54c2f129080816a87cc68ea0c6ba0de930ba6bbcf85064dd6"
  "one_rule.json 88f7b1b6049be88e1e2827673b75fc9261986b216e8ee6bf09621fecbe274e3c"
  "rule_list.json 7a0629ff8747c7837211c2f645a71f32ee69a5ee058ccc4e206d835cd128e55f"
  "stylesheet.json 7292c367370a7ccd4ff47f5c067d692f1ad665340cc7ec4754ea8c3681763794"
  "LICENSE 5f9019a92f4aa8917aadc8e035aa673c2c1bf08d5ca2e535a0564106599f44eb"
)

# verify <file> <sha256> fails when file does not hash to sha256.
verify() {
  local got
  got="$(shasum -a 256 "$1" | cut -d ' ' -f 1)"
  if [ "$got" != "$2" ]; then
    echo "ERROR: $(basename "$1") hashes to $got, the pin is $2" >&2
    exit 1
  fi
}

if [ -f "$OUT/.pinned" ] && [ "$(cat "$OUT/.pinned")" = "$COMMIT" ]; then
  echo "  css-parsing-tests: already at $COMMIT"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for entry in "${FILES[@]}"; do
  name="${entry%% *}"
  sha="${entry##* }"
  curl -fsSL --retry 3 -o "$tmp/$name" "$BASE/$name"
  verify "$tmp/$name" "$sha"
done
rm -rf "$OUT"
mkdir -p "$OUT"
cp "$tmp"/* "$OUT/"
printf '%s' "$COMMIT" > "$OUT/.pinned"
echo "  css-parsing-tests: installed $COMMIT (${#FILES[@]} files)"
