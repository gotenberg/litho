#!/usr/bin/env bash
# run-verapdf.sh: run VeraPDF against a flat pdfs/ directory.
#
# Two modes:
#   1. Cartesian (no --claims): validate every PDF against every
#      profile in the supplied CSV. Used by the VeraPDF corpus where
#      the per-profile reports are the goldens.
#   2. Per-claim (--claims <path>): each PDF is validated only against
#      the conformance profile it declares in its XMP packet. The
#      claims file is "<basename>,<profile>" CSV produced by the
#      `corpus scan-claims` subcommand. Used by govdocs and pdf20 so
#      provisioning cost scales with corpus size, not corpus size ×
#      profile count.
#
# Usage:
#   run-verapdf.sh <pdfs_dir> <compliance_out_dir> <profiles_csv> [--claims <path>]
#
# Emits one JSON report per profile, same shape the compliance feature
# parses (report.jobs[].itemDetails.name / validationResult[]).
set -euo pipefail

PDFS_DIR="$1"
OUT_DIR="$2"
PROFILES="${3:-PDF_A-1a,PDF_A-1b,PDF_A-2a,PDF_A-2b,PDF_A-2u,PDF_A-3a,PDF_A-3b,PDF_A-3u,PDF_A-4,PDF_A-4e,PDF_A-4f,PDF_UA-1,PDF_UA-2}"
CLAIMS=""
shift 3 || true
while [ $# -gt 0 ]; do
  case "$1" in
    --claims)
      CLAIMS="$2"
      shift 2
      ;;
    *)
      echo "  WARN: unknown arg $1, ignoring" >&2
      shift
      ;;
  esac
done

mkdir -p "$OUT_DIR"

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

if [ -n "$CLAIMS" ]; then
  if [ ! -f "$CLAIMS" ]; then
    echo "ERROR: claims file not found at $CLAIMS" >&2
    exit 1
  fi
  STAGE_ROOT=$(mktemp -d -t verapdf-claims.XXXXXX)
  trap 'rm -rf "$STAGE_ROOT"' EXIT

  # Bucket each PDF into a staging directory keyed by its claimed
  # profile. Hard-link so we don't pay copy cost for ~1500 files.
  while IFS=, read -r base profile; do
    [ -n "$base" ] || continue
    [ -n "$profile" ] || continue
    src="$PDFS_DIR/${base}.pdf"
    if [ ! -f "$src" ]; then
      echo "  WARN: claimed PDF missing on disk: ${base}.pdf" >&2
      continue
    fi
    bucket="$STAGE_ROOT/$profile"
    mkdir -p "$bucket"
    ln "$src" "$bucket/${base}.pdf" 2> /dev/null \
      || cp "$src" "$bucket/${base}.pdf"
  done < "$CLAIMS"

  for bucket in "$STAGE_ROOT"/*; do
    [ -d "$bucket" ] || continue
    profile=$(basename "$bucket")
    flavour="${FLAVOURS[$profile]:-}"
    if [ -z "$flavour" ]; then
      echo "  WARN: unknown profile ${profile}, skipping" >&2
      continue
    fi
    count=$(find "$bucket" -maxdepth 1 -name '*.pdf' | wc -l)
    echo "  $profile (flavour=$flavour, ${count} PDFs)..."
    verapdf -f "$flavour" -r --format json --maxfailuresdisplayed -1 "$bucket" \
      > "$OUT_DIR/$profile.json" 2> /dev/null \
      || echo "  WARN: verapdf had errors for $profile (exit $?)"
  done
  exit 0
fi

# Cartesian fallback: every PDF against every requested profile.
IFS=',' read -ra PROFILE_LIST <<< "$PROFILES"
for profile in "${PROFILE_LIST[@]}"; do
  flavour="${FLAVOURS[$profile]:-}"
  if [ -z "$flavour" ]; then
    echo "  WARN: unknown profile ${profile}, skipping"
    continue
  fi
  echo "  $profile (flavour=$flavour)..."
  verapdf -f "$flavour" -r --format json --maxfailuresdisplayed -1 "$PDFS_DIR" \
    > "$OUT_DIR/$profile.json" 2> /dev/null \
    || echo "  WARN: verapdf had errors for $profile (exit $?)"
done
