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
# (text, metadata, imagedecode, render, compliance, remediation,
# remediation-aggressive).
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

# The image records the versions it was built with (versions.env, overrides
# included). Export them so the download scripts read their corpus revision
# from the same place the version record below is written from.
VERSIONS=/opt/litho/versions.env
if [ ! -s "$VERSIONS" ]; then
  echo "ERROR: $VERSIONS missing: build the image through corpus init" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1090
. "$VERSIONS"
set +a

PINS_JSON=$(jq -Rn '[inputs | capture("^(?<k>[A-Z][A-Z0-9_]*)=(?<v>.*)$")] | map({(.k): .v}) | add // {}' < "$VERSIONS")

# tool_versions reports what the image's tools say about themselves. The
# record carries it next to the pins so a golden's provenance can be read
# without the image that made it.
tool_versions() {
  jq -n \
    --arg verapdf "$(verapdf --version 2> /dev/null | head -n 2 | paste -sd ' ' -)" \
    --arg java "$(java -version 2>&1 | head -n 2 | tail -n 1)" \
    --arg node "$(node --version)" \
    --arg pdfjs "$(jq -c . /opt/pdfjs/build/version.json 2> /dev/null || true)" \
    --arg pdfcpu "$(pdfcpu version 2> /dev/null | grep -m 1 '^pdfcpu:' || true)" \
    --arg poppler "$(pdftoppm -v 2>&1 | head -n 1)" \
    '{verapdf: $verapdf, java: $java, node: $node, pdfjs: $pdfjs, pdfcpu: $pdfcpu, poppler: $poppler}'
}
TOOLS_JSON=$(tool_versions)

# record_stage <relative_dir> [extra_json] writes the versions a stage ran
# with into $OUTPUT/versions.json, keyed by the directory the stage filled
# (pdfs, goldens/text, ...). The corpus CLI refuses to score a golden whose
# record disagrees with versions.env. Only a stage that ran is recorded: one
# skipped because its output already exists keeps its old record.
record_stage() {
  local rel="$1" extra="${2:-}" rec="$OUTPUT/versions.json" tmp
  [ -n "$extra" ] || extra='{}'
  tmp=$(mktemp)
  if [ -s "$rec" ]; then
    cat "$rec" > "$tmp"
  else
    echo '{"format": 1, "stages": {}}' > "$tmp"
  fi
  # Plain shell redirect sidesteps the fallocate probe of cp/mv, which
  # breaks on macOS Docker Desktop's virtiofs bind mount.
  jq --arg rel "$rel" \
    --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --argjson pins "$PINS_JSON" \
    --argjson tools "$TOOLS_JSON" \
    --argjson extra "$extra" \
    '.stages[$rel] = ({recorded_at: $at, pins: $pins, tools: $tools} + $extra)' \
    "$tmp" > "$rec"
  rm -f "$tmp"
}

# nonempty <dir> reports whether the directory exists and contains at
# least one entry. Used to short-circuit each provisioning stage so
# rerunning the container after a partial failure (or just bumping the
# image) does not pay the cost of stages that already succeeded.
nonempty() {
  [ -d "$1" ] && [ -n "$(ls -A "$1" 2> /dev/null || true)" ]
}

PDF_COUNT=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
if [ "$PDF_COUNT" -gt 0 ]; then
  echo "==> ${CORPUS}: ${PDF_COUNT} PDFs already on disk, skipping download."
else
  echo "==> ${CORPUS}: download..."
  "$DOWNLOAD" "$PDFS"
  PDF_COUNT=$(find "$PDFS" -maxdepth 1 -name '*.pdf' | wc -l)
  echo "==> ${CORPUS}: ${PDF_COUNT} PDFs ready."
  if [ "$PDF_COUNT" -gt 0 ]; then
    record_stage pdfs
  fi
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
    record_stage goldens/text
    record_stage goldens/metadata
  fi
else
  echo "==> skipping text + metadata goldens (not requested by ${CORPUS})."
fi

if wants imagedecode; then
  IMAGES="$OUTPUT/goldens/images"
  if nonempty "$IMAGES"; then
    echo "==> image goldens already present, skipping PDF.js image decode."
  else
    mkdir -p "$IMAGES"
    echo "==> generating image goldens (PDF.js)..."
    node /work/lib/gen-images.mjs \
      --pdfjs-dir /opt/pdfjs \
      --pdfs-dir "$PDFS" \
      --out "$IMAGES"
    record_stage goldens/images
  fi
else
  echo "==> skipping image goldens (not requested by ${CORPUS})."
fi

if wants render; then
  RENDER="$OUTPUT/goldens/render"
  if nonempty "$RENDER"; then
    echo "==> render goldens already present, skipping pdftoppm."
  else
    # The litho corpus is rendered whole: its fixtures are small and each
    # exercises a construct of its own. The downloaded corpora are rendered
    # one PDF in sixteen, chosen by a hash of the file name (see
    # gen-render.sh), which keeps their goldens to a few megabytes.
    case "$CORPUS" in
      litho) SAMPLE=256 ;;
      *) SAMPLE=16 ;;
    esac
    mkdir -p "$RENDER"
    echo "==> generating render goldens (pdftoppm, ${SAMPLE}/256 of the PDFs)..."
    SHELL=/bin/bash /work/lib/gen-render.sh "$PDFS" "$RENDER" "$SAMPLE"
    record_stage goldens/render "$(jq -nc --argjson s "$SAMPLE" '{render_sample_per_256: $s}')"
  fi
  # The fonts poppler draws unembedded fonts with (URW base 35, DejaVu), the
  # TrueType and OpenType files of its bundle, which the render feature
  # hands to litho so both draw them alike. They come from the Debian release
  # the goldens/render pins name, so the stage's record covers them; testdata
  # whose goldens predate the copy gets it on the next run.
  if ! nonempty "$RENDER/fonts"; then
    echo "==> copying poppler's fonts for the render feature..."
    mkdir -p "$RENDER/fonts"
    # A plain redirect, not cp, as record_stage writes: cp's fallocate probe
    # breaks on macOS Docker Desktop's virtiofs bind mount.
    find /opt/poppler/fonts -type f \( -name '*.otf' -o -name '*.ttf' \) -print0 \
      | while IFS= read -r -d '' font; do
        cat "$font" > "$RENDER/fonts/$(basename "$font")"
      done
  fi
else
  echo "==> skipping render goldens (not requested by ${CORPUS})."
fi

if wants bookmarks; then
  BOOKS="$OUTPUT/goldens/bookmarks"
  if nonempty "$BOOKS"; then
    echo "==> bookmarks goldens already present, skipping pdfcpu export."
  else
    mkdir -p "$BOOKS"
    echo "==> generating bookmarks goldens (pdfcpu)..."
    /work/lib/gen-bookmarks.sh "$PDFS" "$BOOKS"
    record_stage goldens/bookmarks
  fi
else
  echo "==> skipping bookmarks goldens (not requested by ${CORPUS})."
fi

if wants merge; then
  MERGES="$OUTPUT/goldens/merges"
  if nonempty "$MERGES"; then
    echo "==> merge goldens already present, skipping pdfcpu merge."
  else
    mkdir -p "$MERGES"
    echo "==> generating merge goldens (pdfcpu merge + bookmarks export)..."
    /work/lib/gen-merges.sh "$PDFS" "$MERGES"
    record_stage goldens/merges
  fi
else
  echo "==> skipping merge goldens (not requested by ${CORPUS})."
fi

if wants merge-empty; then
  ME="$OUTPUT/goldens/merge-empty"
  if nonempty "$ME"; then
    echo "==> merge-empty goldens already present, skipping pdfcpu merge."
  else
    mkdir -p "$ME"
    echo "==> generating merge-empty goldens (pdfcpu merge + bookmarks export)..."
    /work/lib/gen-merge-empty.sh "$PDFS" "$ME"
    record_stage goldens/merge-empty
  fi
else
  echo "==> skipping merge-empty goldens (not requested by ${CORPUS})."
fi

if wants facturx-validity; then
  FX="$OUTPUT/goldens/facturx-validity"
  if nonempty "$FX"; then
    echo "==> facturx-validity goldens already present, skipping Mustang."
  else
    mkdir -p "$FX"
    echo "==> running Mustang per fixture..."
    /work/lib/run-mustang.sh "$PDFS" "$FX/jobs.json"
    record_stage goldens/facturx-validity
  fi
else
  echo "==> skipping Mustang goldens (not requested by ${CORPUS})."
fi

if wants facturx-schematron; then
  KO="$OUTPUT/goldens/facturx-schematron"
  if nonempty "$KO"; then
    echo "==> facturx-schematron goldens already present, skipping KoSIT."
  else
    mkdir -p "$KO"
    echo "==> running KoSIT per fixture..."
    /work/lib/run-kosit.sh "$PDFS" "$KO/jobs.json"
    record_stage goldens/facturx-schematron
  fi
else
  echo "==> skipping KoSIT goldens (not requested by ${CORPUS})."
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
    # Record the build the reports themselves name: it is what the corpus
    # CLI reads back from a golden that predates the version record.
    BUILD='{}'
    REPORT=$(find "$COMP" -maxdepth 1 -name '*.json' | sort | head -n 1)
    if [ -n "$REPORT" ]; then
      BUILD=$(jq -c '{verapdf_build: .report.buildInformation.releaseDetails}' "$REPORT")
    fi
    record_stage goldens/compliance "$BUILD"
  fi
else
  echo "==> skipping VeraPDF goldens (not requested by ${CORPUS})."
fi

echo "==> ${CORPUS}: done."
