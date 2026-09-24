#!/usr/bin/env bash
# pdf20/download.sh: clone the PDF Association's pdf20examples
# repository and flatten its tree into a single pdfs/ directory. The
# upstream project ships seven PDFs under CC BY-SA 4.0 (its LICENSE.md)
# that exercise PDF 2.0 (ISO 32000-2) features (UTF-8 strings,
# page-level output intents, incremental saves, an offset start,
# etc.). They are the smallest curated set of real PDF 2.0 documents
# available as a canary alongside the (almost exclusively PDF 1.x)
# Govdocs corpus.
#
# Usage: pdf20/download.sh <output_pdfs_dir>
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

git clone --depth 1 https://github.com/pdf-association/pdf20examples.git /tmp/pdf20-examples

# Flatten: prefix each PDF with the relative directory it came from
# so basenames stay unique when subdirs share filenames. Plain shell
# redirect sidesteps cp/install's fallocate probe that breaks on
# macOS Docker Desktop's virtiofs bind mount.
find /tmp/pdf20-examples -type f -iname '*.pdf' -print0 \
  | while IFS= read -r -d '' pdf; do
    rel=${pdf#/tmp/pdf20-examples/}
    safe=$(echo "$rel" | tr '/ ' '__')
    cat "$pdf" > "$OUT/$safe"
  done

rm -rf /tmp/pdf20-examples
