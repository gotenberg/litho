#!/usr/bin/env bash
# pdfcpu/download.sh: fetch the pdfcpu repository at the pinned revision
# (PDFCPU_CORPUS_REVISION, from versions.env) and flatten its
# sample/testdata PDFs into a single pdfs/ directory.
#
# The pdfcpu project (Apache-2.0) ships two parallel asset trees:
#
#   pkg/samples/bookmarks/   feature-curated bookmark/outline fixtures
#   pkg/testdata/            ~48 mixed PDFs (small + multi-page) at the
#                            top level, used as inputs for pdfcpu's own
#                            CLI test suite
#
# We pull only those two locations. Resources (images, ICC profiles,
# fonts) under pkg/testdata/resources and pkg/testdata/fonts are skipped:
# they are not PDFs. The pkg/testdata/pdf20 subdirectory is also skipped
# because the existing pdf20 corpus already covers PDF 2.0 fixtures from
# the PDF Association's reference repo.
#
# Usage: pdfcpu/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

SRC=/tmp/pdfcpu-src
/usr/local/bin/fetch-revision https://github.com/pdfcpu/pdfcpu.git \
  "${PDFCPU_CORPUS_REVISION:?}" "$SRC"

# Iterate two source roots. Prefix each filename with the relative
# directory so basenames stay unique when flattened (e.g. samples/
# and testdata/ can both ship a bookmarkTree.pdf without colliding).
# Plain shell redirect sidesteps cp/install's fallocate probe that
# breaks on macOS Docker Desktop's virtiofs bind mount.
for root in pkg/samples/bookmarks pkg/testdata; do
  if [ ! -d "$SRC/$root" ]; then
    echo "  WARN: ${root} missing from pdfcpu checkout, skipping"
    continue
  fi
  prefix=$(echo "$root" | tr '/ ' '__')
  found=0
  while IFS= read -r -d '' pdf; do
    # Normalise the extension to lowercase; the corpus runner's
    # fixture discovery matches '*.pdf' case-sensitively, so a stray
    # '.PDF' (e.g. pdfcpu ships Paclitaxel.PDF) would be silently
    # dropped from the scored matrix.
    base=$(basename "$pdf")
    stem=${base%.[Pp][Dd][Ff]}
    cat "$pdf" > "$OUT/${prefix}__${stem}.pdf"
    found=$((found + 1))
  done < <(find "$SRC/$root" -maxdepth 1 -type f -iname '*.pdf' -print0)
  echo "  ${root}: ${found} PDFs"
done

rm -rf "$SRC"
