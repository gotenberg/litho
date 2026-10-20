#!/usr/bin/env bash
# bundle-poppler.sh: package the poppler utilities built in the image's
# poppler stage so they run in the final stage, which sits on an older Debian
# release.
#
# The bundle carries each binary with every shared library ldd lists for it,
# the dynamic loader among them, and the build stage's fonts with a
# fontconfig configuration of its own: the build stage's alias rules
# (conf.d), the bundled fonts as the only font directory, and their cache. A
# wrapper per binary runs it through the bundled loader on the bundled
# libraries and points fontconfig at that configuration (FONTCONFIG_FILE), so
# neither the final stage's glibc nor its fonts take part in a rendering.
#
# Usage: bundle-poppler.sh <build_utils_dir> <bundle_dir> <binary>...
set -euo pipefail

BUILD="$1"
BUNDLE="$2"
shift 2

mkdir -p "$BUNDLE/bin" "$BUNDLE/libexec" "$BUNDLE/lib"

loader=""
for name in "$@"; do
  install -m 0755 "$BUILD/$name" "$BUNDLE/libexec/$name"
  # ldd prints "libfoo.so.1 => /path (addr)" for a library and
  # "/lib/ld-linux-<arch>.so.1 (addr)" for the loader.
  while read -r path; do
    cp -L "$path" "$BUNDLE/lib/"
  done < <(ldd "$BUILD/$name" | awk '$2 == "=>" && $3 ~ /^\// { print $3 }')
  found=$(ldd "$BUILD/$name" | awk '$1 ~ /^\/.*ld-linux/ { print $1 }')
  if [ -z "$found" ]; then
    echo "no dynamic loader in ldd $BUILD/$name" >&2
    exit 1
  fi
  loader="$found"
done
cp -L "$loader" "$BUNDLE/lib/ld.so"

for name in "$@"; do
  cat > "$BUNDLE/bin/$name" << EOF
#!/bin/sh
FONTCONFIG_FILE=$BUNDLE/etc/fonts/fonts.conf
export FONTCONFIG_FILE
exec $BUNDLE/lib/ld.so --library-path $BUNDLE/lib $BUNDLE/libexec/$name "\$@"
EOF
  chmod 0755 "$BUNDLE/bin/$name"
done

# fontconfig hands poppler the path of the font file it matched, so the fonts
# must sit at the path the configuration names. conf.d is dereferenced: its
# entries link to /usr/share/fontconfig, which the final stage lacks. Its
# include is absolute: fontconfig resolves a relative one against its own
# configuration directory, /etc/fonts, which in the final stage holds the
# older release's rules without the URW aliases, so Helvetica matched DejaVu
# Sans instead of Nimbus Sans. No directory under $HOME takes part.
mkdir -p "$BUNDLE/etc/fonts" "$BUNDLE/cache"
cp -aL /etc/fonts/conf.d "$BUNDLE/etc/fonts/"
cp -aL /usr/share/fonts "$BUNDLE/fonts"
cat > "$BUNDLE/etc/fonts/fonts.conf" << EOF
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <dir>$BUNDLE/fonts</dir>
  <cachedir>$BUNDLE/cache</cachedir>
  <include>$BUNDLE/etc/fonts/conf.d</include>
</fontconfig>
EOF
FONTCONFIG_FILE="$BUNDLE/etc/fonts/fonts.conf" fc-cache -f
