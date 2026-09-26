#!/usr/bin/env bash
# govdocs/download.sh: fetch a fixed set of Govdocs1 thread archives,
# filter to PDFs, and rename them with a stable thread-qualified ID.
#
# Usage: govdocs/download.sh <output_pdfs_dir>
#
# The thread list is pinned (GOVDOCS_THREADS, comma-separated, from
# versions.env) to keep provisioning reproducible across machines. Bump
# it deliberately when more coverage is needed.
set -euo pipefail

OUT="$1"
mkdir -p "$OUT"

# Fixed thread IDs. Each thread holds ~1000 mixed-format files; roughly
# 20-40% are PDFs. Five threads yields ~1000-2000 PDF fixtures.
IFS=',' read -ra THREADS <<< "${GOVDOCS_THREADS:?}"

BASE="https://digitalcorpora.s3.amazonaws.com/corpora/files/govdocs1/zipfiles"

for thread in "${THREADS[@]}"; do
  archive="/tmp/govdocs-${thread}.zip"
  url="${BASE}/${thread}.zip"
  echo "  fetching thread ${thread}..."
  curl -fSL --retry 3 -o "$archive" "$url"

  stage="/tmp/govdocs-extract-${thread}"
  rm -rf "$stage"
  mkdir -p "$stage"
  unzip -q "$archive" -d "$stage" || {
    echo "  WARN: unzip reported errors for thread ${thread}, continuing"
  }

  # Govdocs filenames are numeric (e.g. 042123.pdf). Prefix the thread
  # ID so fixture IDs are unique across threads and stable across
  # re-provisioning. Plain shell redirect sidesteps cp/install's
  # fallocate probe that breaks on macOS Docker Desktop's virtiofs.
  found=0
  while IFS= read -r -d '' pdf; do
    base=$(basename "$pdf" .pdf)
    cat "$pdf" > "$OUT/thread_${thread}_${base}.pdf"
    found=$((found + 1))
  done < <(find "$stage" -type f -iname '*.pdf' -print0)
  echo "  thread ${thread}: ${found} PDFs"

  rm -rf "$archive" "$stage"
done
