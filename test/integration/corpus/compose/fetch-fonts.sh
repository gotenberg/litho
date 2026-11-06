#!/usr/bin/env bash
# compose/fetch-fonts.sh: download the fonts the compose fixtures are built
# with, at the releases versions.env pins.
#
# Each archive or font file is checked against its pinned SHA-256 before
# anything is unpacked, and a mismatch fails the run. A family whose
# directory already records the pinned hash is left alone. Only the TrueType
# and OpenType files and the licence are kept: the builder hands the whole
# directory to pdf.WithConvertFontDirs, so any other face in it could change
# which one a character is drawn with. An environment variable of the same
# name overrides a pin, as everywhere versions.env is read.
#
# Usage: compose/fetch-fonts.sh <fonts_dir>
set -euo pipefail

OUT="${1:?usage: fetch-fonts.sh <fonts_dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSIONS="$HERE/../versions.env"

# pin <KEY> prints the environment's value of KEY, or the one versions.env
# pins, and fails when neither has one.
pin() {
  local v="${!1:-}"
  if [ -z "$v" ]; then
    v="$(sed -n "s/^$1=//p" "$VERSIONS")"
  fi
  if [ -z "$v" ]; then
    echo "ERROR: $1 is not pinned in $VERSIONS" >&2
    exit 1
  fi
  printf '%s' "$v"
}

# verify <file> <sha256> <pin_key> fails when file does not hash to sha256.
verify() {
  local got
  got="$(shasum -a 256 "$1" | cut -d ' ' -f 1)"
  if [ "$got" != "$2" ]; then
    echo "ERROR: $(basename "$1") hashes to $got, $3 pins $2" >&2
    exit 1
  fi
}

# fetch <url> <dest> downloads url to dest, failing on an HTTP error.
fetch() {
  curl -fsSL --retry 3 -o "$2" "$1"
}

# current <dir> <stamp> reports whether dir already holds the family the
# stamp names.
current() {
  [ -f "$1/.pinned" ] && [ "$(cat "$1/.pinned")" = "$2" ]
}

# install_archive <name> <url> <sha256> <pin_key> <tar_flag> fetches a release
# tarball and unpacks its font files and licence into $OUT/<name>.
install_archive() {
  local name="$1" url="$2" sha="$3" key="$4" flag="$5" dir="$OUT/$1" stamp tmp
  stamp="$url $sha"
  if current "$dir" "$stamp"; then
    echo "  fonts: $name already at the pinned release"
    return
  fi
  tmp="$(mktemp -d)"
  fetch "$url" "$tmp/archive"
  verify "$tmp/archive" "$sha" "$key"
  mkdir -p "$tmp/x"
  tar -x"$flag"f "$tmp/archive" -C "$tmp/x"
  rm -rf "$dir"
  mkdir -p "$dir"
  find "$tmp/x" -type f \( -name '*.ttf' -o -name '*.otf' -o -name 'LICENSE' \) -exec cp {} "$dir/" \;
  printf '%s' "$stamp" > "$dir/.pinned"
  rm -rf "$tmp"
  echo "  fonts: $name installed ($(find "$dir" -name '*.[ot]tf' | wc -l | tr -d ' ') faces)"
}

# install_zip <name> <url> <sha256> <pin_key> <member>... fetches a release
# zip and unpacks the named members, flattened, into $OUT/<name>, OFL.txt
# becoming LICENSE.
install_zip() {
  local name="$1" url="$2" sha="$3" key="$4" dir="$OUT/$1" stamp tmp
  shift 4
  stamp="$url $sha"
  if current "$dir" "$stamp"; then
    echo "  fonts: $name already at the pinned release"
    return
  fi
  tmp="$(mktemp -d)"
  fetch "$url" "$tmp/archive"
  verify "$tmp/archive" "$sha" "$key"
  mkdir -p "$tmp/x"
  unzip -q -j "$tmp/archive" "$@" OFL.txt -d "$tmp/x"
  mv "$tmp/x/OFL.txt" "$tmp/x/LICENSE"
  rm -rf "$dir"
  mkdir -p "$dir"
  cp "$tmp"/x/* "$dir/"
  printf '%s' "$stamp" > "$dir/.pinned"
  rm -rf "$tmp"
  echo "  fonts: $name installed ($(find "$dir" -name '*.[ot]tf' | wc -l | tr -d ' ') faces)"
}

mkdir -p "$OUT"

install_archive "liberation-$(pin LIBERATION_FONTS_VERSION)" \
  "$(pin LIBERATION_FONTS_URL)" "$(pin LIBERATION_FONTS_SHA256)" LIBERATION_FONTS_SHA256 z
install_archive "dejavu-$(pin DEJAVU_FONTS_VERSION)" \
  "$(pin DEJAVU_FONTS_URL)" "$(pin DEJAVU_FONTS_SHA256)" DEJAVU_FONTS_SHA256 j

# Noto Sans CJK: the Regular face of each region's subset OpenType font, from
# the tag and at the URLs the corpus image installs them from (Dockerfile,
# poppler-build stage), checked against the same per-region pins.
NOTO="$(pin NOTO_CJK_SANS_VERSION)"
dir="$OUT/noto-cjk-sans-$NOTO"
base="https://github.com/notofonts/noto-cjk/raw/Sans${NOTO}"
stamp="$base"
for region in JP KR SC TC; do
  stamp="$stamp $(pin "NOTO_SANS_${region}_SHA256")"
done
if current "$dir" "$stamp"; then
  echo "  fonts: noto-cjk-sans-$NOTO already at the pinned release"
else
  tmp="$(mktemp -d)"
  for region in JP KR SC TC; do
    file="NotoSans${region}-Regular.otf"
    fetch "$base/Sans/SubsetOTF/$region/$file" "$tmp/$file"
    verify "$tmp/$file" "$(pin "NOTO_SANS_${region}_SHA256")" "NOTO_SANS_${region}_SHA256"
  done
  fetch "$base/LICENSE" "$tmp/LICENSE"
  rm -rf "$dir"
  mkdir -p "$dir"
  cp "$tmp"/* "$dir/"
  printf '%s' "$stamp" > "$dir/.pinned"
  rm -rf "$tmp"
  echo "  fonts: noto-cjk-sans-$NOTO installed (4 faces)"
fi

# Noto Sans Thai and Noto Sans Devanagari: the hinted static Regular and Bold
# faces of each release archive.
THAI="$(pin NOTO_SANS_THAI_VERSION)"
install_zip "noto-sans-thai-$THAI" "$(pin NOTO_SANS_THAI_URL)" "$(pin NOTO_SANS_THAI_SHA256)" NOTO_SANS_THAI_SHA256 \
  NotoSansThai/hinted/ttf/NotoSansThai-Regular.ttf NotoSansThai/hinted/ttf/NotoSansThai-Bold.ttf
DEVANAGARI="$(pin NOTO_SANS_DEVANAGARI_VERSION)"
install_zip "noto-sans-devanagari-$DEVANAGARI" "$(pin NOTO_SANS_DEVANAGARI_URL)" "$(pin NOTO_SANS_DEVANAGARI_SHA256)" NOTO_SANS_DEVANAGARI_SHA256 \
  NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Regular.ttf NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Bold.ttf
