#!/usr/bin/env bash
# litho/download.sh: install the committed litho corpus fixtures.
#
# Unlike every other corpus, this one downloads nothing. The fixtures are
# self-authored, tiny, and carry no third-party licence, so they live in
# git next to the pikepdf builders that generate them (see build/). The
# provisioning image has no Python, so this step is a plain copy from the
# corpus source directory the Dockerfile bakes in at /work/litho.
#
# Usage: litho/download.sh <output_pdfs_dir>
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
echo "  litho: ${count} committed fixtures installed"
