#!/usr/bin/env bash
# entrypoint.sh: shared provisioning entrypoint.
#
# Dispatches to <corpus>/download.sh for the download step, then runs
# only the golden generators relevant to the requested feature set.
# Invoked as:
#
#   docker run <image> <corpus_name> <profiles_csv> <features_csv> <profile_mode>
#
# features_csv lists the feature names whose goldens this corpus needs
# (text, metadata, compliance, remediation, remediation-aggressive).
# profile_mode mirrors the manifest's profile_mode field; "auto"
# triggers the per-claim VeraPDF flow which validates each PDF only
# against the profile it declares in its XMP packet.
set -euo pipefail

CORPUS="${1:-}"
PROFILES="${2:-}"
FEATURES="${3:-text,metadata,compliance,remediation,remediation-aggressive}"
PROFILE_MODE="${4:-expand}"

if [ -z "$CORPUS" ]; then
  echo "usage: entrypoint.sh <corpus_name> <profiles_csv> [features_csv] [profile_mode]" >&2
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

# nonempty <dir> reports whether the directory exists and contains at
# least one entry. Used to short-circuit each provisioning stage so
# rerunning the container after a partial failure (or just bumping the
# image) does not pay the cost of stages that already succeeded.
nonempty() {
  [ -d "$1" ] && [ -n "$(ls -A "$1" 2>/dev/null || true)" ]
}

PDF_COUNT=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
if [ "$PDF_COUNT" -gt 0 ]; then
  echo "==> ${CORPUS}: ${PDF_COUNT} PDFs already on disk, skipping download."
else
  echo "==> ${CORPUS}: download..."
  "$DOWNLOAD" "$PDFS"
  PDF_COUNT=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
  echo "==> ${CORPUS}: ${PDF_COUNT} PDFs ready."
fi
if [ "$PDF_COUNT" -eq 0 ]; then
  echo "ERROR: no PDFs extracted for ${CORPUS}." >&2
  exit 1
fi

CLAIMS=""
if [ "$PROFILE_MODE" = "auto" ]; then
  if [ -x /work/bin/scan-claims ]; then
    CLAIMS="$OUTPUT/claims.csv"
    if [ -s "$CLAIMS" ]; then
      echo "==> reusing existing claims.csv ($(wc -l < "$CLAIMS") rows)."
    else
      echo "==> scanning XMP claims (auto mode)..."
      /work/bin/scan-claims --pdfs "$PDFS" --out "$CLAIMS" --profiles "$PROFILES"
    fi
  else
    echo "==> WARN: profile_mode=auto but /work/bin/scan-claims missing; falling back to cartesian VeraPDF" >&2
  fi
fi

if wants text || wants metadata; then
  TEXT="$OUTPUT/goldens/text"
  META="$OUTPUT/goldens/metadata"
  if nonempty "$TEXT" && nonempty "$META"; then
    echo "==> text + metadata goldens already present, skipping PDF.js generator."
  else
    mkdir -p "$TEXT" "$META"
    echo "==> generating text + metadata goldens (PDF.js)..."
    node /work/lib/gen-references.mjs \
      --pdfjs-dir /opt/pdfjs \
      --pdfs-dir "$PDFS" \
      --text-out "$TEXT" \
      --meta-out "$META"
  fi
else
  echo "==> skipping text + metadata goldens (not requested by ${CORPUS})."
fi

if wants compliance || wants remediation || wants remediation-aggressive; then
  COMP="$OUTPUT/goldens/compliance"
  if nonempty "$COMP"; then
    echo "==> compliance goldens already present, skipping VeraPDF."
  else
    mkdir -p "$COMP"
    if [ -n "$CLAIMS" ]; then
      echo "==> running VeraPDF per claimed profile..."
      /work/lib/run-verapdf.sh "$PDFS" "$COMP" "$PROFILES" --claims "$CLAIMS"
    else
      echo "==> running VeraPDF per profile (cartesian)..."
      /work/lib/run-verapdf.sh "$PDFS" "$COMP" "$PROFILES"
    fi
  fi
else
  echo "==> skipping VeraPDF goldens (not requested by ${CORPUS})."
fi

echo "==> ${CORPUS}: done."
