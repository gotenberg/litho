#!/usr/bin/env bash
# html/download.sh: install the committed Chromium reference PDFs.
#
# The html corpus downloads nothing. Its fixtures are the HTML sources under
# sources/, and the PDFs it provisions are the Chromium conversions of them,
# chromium/<name>.pdf, which `make html-chromium` builds with the Gotenberg
# image versions.env pins and git versions. The provisioning image derives the
# diff goldens from them; litho's side converts the sources on the host. This
# step is a plain copy from the corpus source directory the Dockerfile bakes
# in at /work/html.
#
# Usage: html/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/chromium"
if [ ! -d "$SRC" ]; then
  echo "ERROR: reference directory not found at $SRC: run make html-chromium" >&2
  exit 1
fi

count=0
for pdf in "$SRC"/*.pdf; do
  [ -f "$pdf" ] || continue
  # Plain shell redirect sidesteps cp/install's fallocate probe, which
  # breaks on macOS Docker Desktop's virtiofs bind mount.
  cat "$pdf" > "$OUT/$(basename "$pdf")"
  count=$((count + 1))
done

if [ "$count" -eq 0 ]; then
  echo "ERROR: no Chromium reference under $SRC: run make html-chromium" >&2
  exit 1
fi
echo "  html: ${count} Chromium references installed"
