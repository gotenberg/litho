#!/usr/bin/env bash
# entrypoint.sh: shared provisioning entrypoint.
#
# Dispatches to <corpus>/download.sh for the download step, then runs
# the shared golden generators. Invoked as:
#
#   docker run <image> <corpus_name> <profiles_csv>
set -euo pipefail

CORPUS="${1:-}"
PROFILES="${2:-}"

if [ -z "$CORPUS" ]; then
  echo "usage: entrypoint.sh <corpus_name> <profiles_csv>" >&2
  exit 1
fi

DOWNLOAD="/work/${CORPUS}/download.sh"
if [ ! -x "$DOWNLOAD" ]; then
  echo "no download.sh for corpus ${CORPUS} at ${DOWNLOAD}" >&2
  exit 1
fi

OUTPUT="/output"
PDFS="$OUTPUT/pdfs"
TEXT="$OUTPUT/goldens/text"
META="$OUTPUT/goldens/metadata"
COMP="$OUTPUT/goldens/compliance"
mkdir -p "$PDFS" "$TEXT" "$META" "$COMP"

echo "==> ${CORPUS}: download..."
"$DOWNLOAD" "$PDFS"

TOTAL=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
echo "==> ${CORPUS}: ${TOTAL} PDFs ready."
if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no PDFs extracted for ${CORPUS}." >&2
  exit 1
fi

echo "==> generating text + metadata goldens (PDF.js)..."
node /work/lib/gen-references.mjs \
  --pdfjs-dir /opt/pdfjs \
  --pdfs-dir "$PDFS" \
  --text-out "$TEXT" \
  --meta-out "$META"

echo "==> running VeraPDF per profile..."
/work/lib/run-verapdf.sh "$PDFS" "$COMP" "$PROFILES"

echo "==> ${CORPUS}: done."
