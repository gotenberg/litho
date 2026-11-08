#!/usr/bin/env bash
# lib/fetch-fonts.sh: download the fixture fonts litho builds the converter
# corpora with, at the releases versions.env pins.
#
# Each archive or font file is checked against its pinned SHA-256 before
# anything is unpacked, and a mismatch fails the run. A family whose
# directory already records the pinned hash is left alone. Only the TrueType
# and OpenType files and the licence are kept: the builder hands the whole
# directory to pdf.WithConvertFontDirs, so any other face in it could change
# which one a character is drawn with. An environment variable of the same
# name overrides a pin, as everywhere versions.env is read.
#
# Usage: lib/fetch-fonts.sh <fonts_dir>
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

# install_tar <name> <url> <sha256> <pin_key> <prefix> <member>... fetches a
# gzip tarball and unpacks the named members of its <prefix>/ directory,
# flattened, into $OUT/<name>, OFL.txt becoming LICENSE.
install_tar() {
  local name="$1" url="$2" sha="$3" key="$4" prefix="$5" dir="$OUT/$1" stamp tmp m
  shift 5
  stamp="$url $sha"
  if current "$dir" "$stamp"; then
    echo "  fonts: $name already at the pinned release"
    return
  fi
  tmp="$(mktemp -d)"
  fetch "$url" "$tmp/archive"
  verify "$tmp/archive" "$sha" "$key"
  mkdir -p "$tmp/x"
  local members=("$prefix/OFL.txt")
  for m in "$@"; do
    members+=("$prefix/$m")
  done
  tar -xzf "$tmp/archive" -C "$tmp/x" "${members[@]}"
  rm -rf "$dir"
  mkdir -p "$dir"
  for m in "${members[@]}"; do
    cp "$tmp/x/$m" "$dir/"
  done
  mv "$dir/OFL.txt" "$dir/LICENSE"
  printf '%s' "$stamp" > "$dir/.pinned"
  rm -rf "$tmp"
  echo "  fonts: $name installed ($(find "$dir" -name '*.[ot]tf' | wc -l | tr -d ' ') faces)"
}

# install_deb <name> <url> <sha256> <pin_key> <licence> <member>... fetches
# a Debian package and unpacks the named members of its data archive and the
# licence file, flattened, into $OUT/<name>, the licence becoming LICENSE.
# The data archive is an xz tarball, as Debian's font packages build it.
install_deb() {
  local name="$1" url="$2" sha="$3" key="$4" licence="$5" dir="$OUT/$1" stamp tmp m
  shift 5
  stamp="$url $sha"
  if current "$dir" "$stamp"; then
    echo "  fonts: $name already at the pinned release"
    return
  fi
  tmp="$(mktemp -d)"
  fetch "$url" "$tmp/archive"
  verify "$tmp/archive" "$sha" "$key"
  mkdir -p "$tmp/x"
  (cd "$tmp" && ar x archive data.tar.xz)
  local members=("./$licence")
  for m in "$@"; do
    members+=("./$m")
  done
  tar -xJf "$tmp/data.tar.xz" -C "$tmp/x" "${members[@]}"
  rm -rf "$dir"
  mkdir -p "$dir"
  for m in "$@"; do
    cp "$tmp/x/$m" "$dir/"
  done
  cp "$tmp/x/$licence" "$dir/LICENSE"
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

# Noto Sans CJK Regular, the collection of every region's face, from the same
# tag: the face the Gotenberg image's fontconfig gives CJK text. Its directory
# shares the noto-cjk-sans- prefix, so the converter oracles index it with the
# subset faces.
dir="$OUT/noto-cjk-sans-otc-$NOTO"
url="$base/Sans/OTC/NotoSansCJK-Regular.ttc"
stamp="$url $(pin NOTO_SANS_CJK_OTC_SHA256)"
if current "$dir" "$stamp"; then
  echo "  fonts: noto-cjk-sans-otc-$NOTO already at the pinned release"
else
  tmp="$(mktemp -d)"
  fetch "$url" "$tmp/NotoSansCJK-Regular.ttc"
  verify "$tmp/NotoSansCJK-Regular.ttc" "$(pin NOTO_SANS_CJK_OTC_SHA256)" NOTO_SANS_CJK_OTC_SHA256
  fetch "$base/LICENSE" "$tmp/LICENSE"
  rm -rf "$dir"
  mkdir -p "$dir"
  cp "$tmp"/* "$dir/"
  printf '%s' "$stamp" > "$dir/.pinned"
  rm -rf "$tmp"
  echo "  fonts: noto-cjk-sans-otc-$NOTO installed"
fi

# Noto Sans Thai and Noto Sans Devanagari: the hinted static Regular and Bold
# faces of each release archive.
THAI="$(pin NOTO_SANS_THAI_VERSION)"
install_zip "noto-sans-thai-$THAI" "$(pin NOTO_SANS_THAI_URL)" "$(pin NOTO_SANS_THAI_SHA256)" NOTO_SANS_THAI_SHA256 \
  NotoSansThai/hinted/ttf/NotoSansThai-Regular.ttf NotoSansThai/hinted/ttf/NotoSansThai-Bold.ttf
DEVANAGARI="$(pin NOTO_SANS_DEVANAGARI_VERSION)"
install_zip "noto-sans-devanagari-$DEVANAGARI" "$(pin NOTO_SANS_DEVANAGARI_URL)" "$(pin NOTO_SANS_DEVANAGARI_SHA256)" NOTO_SANS_DEVANAGARI_SHA256 \
  NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Regular.ttf NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Bold.ttf

# Carlito and Caladea, the faces the Gotenberg image draws Calibri and Cambria
# with: the four TrueType faces of each Debian orig tarball (Caladea's also
# ships OpenType copies, which would index each face twice).
CARLITO="$(pin CARLITO_FONTS_VERSION)"
install_tar "carlito-$CARLITO" "$(pin CARLITO_FONTS_URL)" "$(pin CARLITO_FONTS_SHA256)" CARLITO_FONTS_SHA256 \
  "fonts-crosextra-carlito-$CARLITO" \
  fonts/ttf/Carlito-Regular.ttf fonts/ttf/Carlito-Bold.ttf fonts/ttf/Carlito-Italic.ttf fonts/ttf/Carlito-BoldItalic.ttf
CALADEA="$(pin CALADEA_FONTS_VERSION)"
install_tar "caladea-$CALADEA" "$(pin CALADEA_FONTS_URL)" "$(pin CALADEA_FONTS_SHA256)" CALADEA_FONTS_SHA256 \
  "fonts-crosextra-caladea-$CALADEA" \
  fonts/ttf/Caladea-Regular.ttf fonts/ttf/Caladea-Bold.ttf fonts/ttf/Caladea-Italic.ttf fonts/ttf/Caladea-BoldItalic.ttf

# Noto Sans and Noto Serif, the faces the Gotenberg image draws a family it
# lacks with, a sans with the first and a serif with the second: the four
# faces of each from Debian's fonts-noto-core, the image's package. The
# directory held the Sans faces alone under its old name, which is removed
# so a tree fetched before the Serif faces joined does not index two copies.
NOTO_CORE="$(pin NOTO_SANS_CORE_VERSION)"
rm -rf "$OUT/noto-sans-core-$NOTO_CORE"
install_deb "noto-core-$NOTO_CORE" "$(pin NOTO_SANS_CORE_URL)" "$(pin NOTO_SANS_CORE_SHA256)" NOTO_SANS_CORE_SHA256 \
  usr/share/doc/fonts-noto-core/copyright \
  usr/share/fonts/truetype/noto/NotoSans-Regular.ttf usr/share/fonts/truetype/noto/NotoSans-Bold.ttf \
  usr/share/fonts/truetype/noto/NotoSans-Italic.ttf usr/share/fonts/truetype/noto/NotoSans-BoldItalic.ttf \
  usr/share/fonts/truetype/noto/NotoSerif-Regular.ttf usr/share/fonts/truetype/noto/NotoSerif-Bold.ttf \
  usr/share/fonts/truetype/noto/NotoSerif-Italic.ttf usr/share/fonts/truetype/noto/NotoSerif-BoldItalic.ttf

# OpenSymbol, LibreOffice's symbol font, which the image draws most of a
# presentation's bullets with: Debian's fonts-opensymbol, the image's
# package.
install_deb "opensymbol-$(pin OPENSYMBOL_VERSION)" "$(pin OPENSYMBOL_URL)" "$(pin OPENSYMBOL_SHA256)" OPENSYMBOL_SHA256 \
  usr/share/doc/fonts-opensymbol/copyright \
  usr/share/fonts/truetype/libreoffice/opens___.ttf
