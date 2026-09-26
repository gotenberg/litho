#!/usr/bin/env bash
# facturx/download.sh: fetch the permissively-licensed Factur-X /
# ZUGFeRD test repository at the pinned revision
# (FACTURX_CORPUS_REVISION, from versions.env) and flatten the relevant
# PDFs into a single pdfs/ directory.
#
# Source:
#
#   ZUGFeRD/corpus (Apache-2.0)
#       Curated collection of real, sample, and intentionally-broken
#       invoices contributed by FNFE-MPE, FeRD, Mustang, Intarsys, and
#       others. Subdirectories are grouped by generation
#       (ZUGFeRDv1, ZUGFeRDv2) and locale variant (XML-Rechnung).
#       Pass/fail status lives in the path itself so a flattened
#       filename like "v2_correct__..." or "v2_fail__..." preserves
#       the oracle's expected verdict for the diff readout.
#
# Out-of-scope subtrees: PEPPOL, fatturaPA, unstructured, incoming,
# and other carry non-CII / non-Factur-X invoices litho does not
# claim to validate.
#
# Usage: facturx/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

SRC=/tmp/facturx-zugferd-corpus

if ! /usr/local/bin/fetch-revision https://github.com/ZUGFeRD/corpus.git \
  "${FACTURX_CORPUS_REVISION:?}" "$SRC"; then
  echo "  WARN: fetch of ZUGFeRD/corpus at ${FACTURX_CORPUS_REVISION} failed"
  exit 1
fi

# extract_subtree <relative-subdir> <prefix>: copy every PDF under
# SRC/<subdir> into OUT with a "<prefix>__<safe-relpath>.pdf" name
# so basenames remain unique and the source folder is recoverable
# from the filename.
extract_subtree() {
  local subdir="$1"
  local prefix="$2"
  local found=0
  if [ ! -d "$SRC/$subdir" ]; then
    echo "  ${prefix}: subtree missing, skipping"
    return 0
  fi
  while IFS= read -r -d '' pdf; do
    local rel base stem safe
    rel="${pdf#$SRC/$subdir/}"
    base=$(basename "$pdf")
    stem="${base%.[Pp][Dd][Ff]}"
    safe=$(printf '%s' "${rel%/*}/${stem}" | tr '/ ' '__')
    cat "$pdf" > "$OUT/${prefix}__${safe}.pdf"
    found=$((found + 1))
  done < <(find "$SRC/$subdir" -type f -iname '*.pdf' -print0)
  echo "  ${prefix}: ${found} PDFs"
}

extract_subtree ZUGFeRDv2 v2
extract_subtree XML-Rechnung xmlrechnung
extract_subtree ZUGFeRDv1 v1

rm -rf "$SRC"
