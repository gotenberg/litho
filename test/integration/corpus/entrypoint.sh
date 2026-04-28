#!/usr/bin/env bash
# entrypoint.sh: shared provisioning entrypoint.
#
# Dispatches to <corpus>/download.sh for the download step, then runs
# only the golden generators relevant to the requested feature set.
# Invoked as:
#
#   docker run <image> <corpus_name> <profiles_csv> <features_csv>
#
# features_csv lists the feature names whose goldens this corpus needs
# (text, metadata, compliance, remediation, remediation-aggressive).
# The runner derives this list from the manifest's "goldens" map and
# passes it through; corpora that don't need PDF.js text/metadata or
# VeraPDF compliance skip those generators outright.
set -euo pipefail

CORPUS="${1:-}"
PROFILES="${2:-}"
FEATURES="${3:-text,metadata,compliance,remediation,remediation-aggressive}"

if [ -z "$CORPUS" ]; then
  echo "usage: entrypoint.sh <corpus_name> <profiles_csv> [features_csv]" >&2
  exit 1
fi

DOWNLOAD="/work/${CORPUS}/download.sh"
if [ ! -x "$DOWNLOAD" ]; then
  echo "no download.sh for corpus ${CORPUS} at ${DOWNLOAD}" >&2
  exit 1
fi

# wants <feature> tests whether a feature appears in the comma-separated
# FEATURES list. Returns 0 (true) when present.
wants() {
  case ",${FEATURES}," in
    *",$1,"*) return 0 ;;
  esac
  return 1
}

OUTPUT="/output"
PDFS="$OUTPUT/pdfs"
mkdir -p "$PDFS"

echo "==> ${CORPUS}: download..."
"$DOWNLOAD" "$PDFS"

TOTAL=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
echo "==> ${CORPUS}: ${TOTAL} PDFs ready."
if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no PDFs extracted for ${CORPUS}." >&2
  exit 1
fi

if wants text || wants metadata; then
  TEXT="$OUTPUT/goldens/text"
  META="$OUTPUT/goldens/metadata"
  mkdir -p "$TEXT" "$META"
  echo "==> generating text + metadata goldens (PDF.js)..."
  node /work/lib/gen-references.mjs \
    --pdfjs-dir /opt/pdfjs \
    --pdfs-dir "$PDFS" \
    --text-out "$TEXT" \
    --meta-out "$META"
else
  echo "==> skipping text + metadata goldens (not requested by ${CORPUS})."
fi

if wants compliance || wants remediation || wants remediation-aggressive; then
  COMP="$OUTPUT/goldens/compliance"
  mkdir -p "$COMP"
  echo "==> running VeraPDF per profile..."
  /work/lib/run-verapdf.sh "$PDFS" "$COMP" "$PROFILES"
else
  echo "==> skipping VeraPDF goldens (not requested by ${CORPUS})."
fi

echo "==> ${CORPUS}: done."
