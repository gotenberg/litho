#!/usr/bin/env bash
# facturx/download.sh: shallow-clone permissively-licensed Factur-X /
# ZUGFeRD test repositories and flatten any PDFs found into a single
# pdfs/ directory.
#
# Sources:
#
#   akretion/factur-x (MIT)
#       Python implementation of Factur-X with a tests/files directory
#       containing samples for every conformance level (MINIMUM through
#       EXTENDED). The reference oracle most third-party Factur-X
#       implementations regression against.
#
#   pdf-association/factur-x (mixed permissive)
#       Joint FNFE-MPE / FeRD repository hosting official Factur-X
#       1.0.07 samples plus a few XRECHNUNG fixtures.
#
# Legacy ZUGFeRD 1.0 / 2.0 samples are intentionally omitted: the only
# canonical sources (FeRD's own samples) are CC BY-ND, which is not a
# corpus-friendly license. Litho's writer + validator paths still
# handle those generations on customer-supplied files.
#
# Usage: facturx/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

# fetch_repo <url> <prefix>: shallow-clone the repo, find every PDF
# (case-insensitive), and copy each into OUT with a stable
# <prefix>__<relative-path>.pdf identifier so basenames stay unique
# across sources. Soft-fails the clone so an unreachable upstream does
# not kill the rest of the provisioning.
fetch_repo() {
  local url="$1"
  local prefix="$2"
  local src="/tmp/facturx-${prefix}"
  rm -rf "$src"

  if ! git clone --depth 1 "$url" "$src" 2> /dev/null; then
    echo "  WARN: clone of ${url} failed, skipping ${prefix}"
    return 0
  fi

  local found=0
  while IFS= read -r -d '' pdf; do
    local rel base stem safe
    rel="${pdf#$src/}"
    base=$(basename "$pdf")
    stem="${base%.[Pp][Dd][Ff]}"
    safe=$(printf '%s' "${rel%/*}/${stem}" | tr '/ ' '__')
    cat "$pdf" > "$OUT/${prefix}__${safe}.pdf"
    found=$((found + 1))
  done < <(find "$src" -type f -iname '*.pdf' -print0)
  echo "  ${prefix}: ${found} PDFs"

  rm -rf "$src"
}

fetch_repo https://github.com/akretion/factur-x.git akretion
fetch_repo https://github.com/pdf-association/factur-x.git pdf-association
