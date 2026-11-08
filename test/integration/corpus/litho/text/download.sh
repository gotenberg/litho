#!/usr/bin/env bash
# litho/text/download.sh: install the committed litho/text corpus fixtures.
#
# Like the litho/pdf corpus, this one downloads nothing. The fixtures are PDFs
# litho itself converted from the plain text sources in sources/, with the
# Go builder in build/ and the pinned fonts `make text-fixtures` fetches;
# they live in git so the goldens never depend on the host's fonts. This step
# is a plain copy from the corpus source directory the Dockerfile bakes in at
# /work/litho/text.
#
# Usage: litho/text/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/pdfs"
if [ ! -d "$SRC" ]; then
  echo "ERROR: fixture directory not found at $SRC" >&2
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
  echo "ERROR: no fixtures under $SRC" >&2
  exit 1
fi
echo "  litho/text: ${count} committed fixtures installed"
