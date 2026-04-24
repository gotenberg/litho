#!/usr/bin/env bash
# run-verapdf.sh: run VeraPDF against a flat pdfs/ directory for every
# conformance profile litho cross-checks against.
#
# Usage: run-verapdf.sh <pdfs_dir> <compliance_out_dir>
#
# Emits one JSON report per profile, same shape the compliance feature
# already parses (report.jobs[].itemDetails.name / validationResult[]).
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
mkdir -p "$OUT_DIR"

# Profile directory name → VeraPDF flavour flag. Matches the verapdf
# corpus's own mapping so the compliance feature indexes one format
# regardless of which corpus produced the report.
declare -A FLAVOURS=(
  ["PDF_A-1a"]="1a"
  ["PDF_A-1b"]="1b"
  ["PDF_A-2a"]="2a"
  ["PDF_A-2b"]="2b"
  ["PDF_A-2u"]="2u"
  ["PDF_A-3a"]="3a"
  ["PDF_A-3b"]="3b"
  ["PDF_A-3u"]="3u"
  ["PDF_A-4"]="4"
  ["PDF_A-4e"]="4e"
  ["PDF_A-4f"]="4f"
  ["PDF_UA-1"]="ua1"
  ["PDF_UA-2"]="ua2"
)

for profile in "${!FLAVOURS[@]}"; do
  flavour="${FLAVOURS[$profile]}"
  echo "  $profile (flavour=$flavour)..."
  verapdf -f "$flavour" -r --format json --maxfailuresdisplayed -1 "$PDFS_DIR" \
    > "$OUT_DIR/$profile.json" 2> /dev/null \
    || echo "  WARN: verapdf had errors for $profile (exit $?)"
done
