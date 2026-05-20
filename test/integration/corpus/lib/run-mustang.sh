#!/usr/bin/env bash
# run-mustang.sh: validate every PDF in a directory with Mustang and
# emit one JSON report per corpus run.
#
# Mustang covers the Factur-X / ZUGFeRD layer that VeraPDF does not see:
# embedded filename, /AFRelationship, catalog /AF, XMP fx:* properties
# plus the extension schema, the embedded stream /Subtype, and the
# Cross-Industry Invoice XSD.
#
# The CLI is invoked once per fixture. Mustang's exit code is 0 for a
# valid invoice and non-zero otherwise; the raw stdout (Mustang's text
# report) is captured into the JSON so the matching scoreboard feature
# can surface failed-rule context in the diff view without re-running
# the validator.
#
# Output shape mirrors the VeraPDF goldens just enough that the matrix
# scoreboard's per-job iteration code stays uniform:
#
#   {
#     "jobs": [
#       {"file": "<basename>.pdf", "valid": true,  "report": "..."},
#       {"file": "<basename>.pdf", "valid": false, "report": "..."}
#     ]
#   }
#
# Usage: run-mustang.sh <pdfs_dir> <out_file>
set -euo pipefail

PDFS_DIR="$1"
OUT_FILE="$2"

if [ ! -f /opt/mustang.jar ]; then
  echo "ERROR: /opt/mustang.jar missing from corpus image" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT_FILE")"

STAGE=$(mktemp -d -t mustang-jobs.XXXXXX)
trap 'rm -rf "$STAGE"' EXIT

count=0
while IFS= read -r -d '' pdf; do
  base=$(basename "$pdf")
  # Capture stdout+stderr; key off the exit code rather than the report
  # text so wording shifts across Mustang releases don't flip verdicts.
  # The "report=$(...) && rc=0 || rc=$?" idiom is necessary because
  # putting the failure-swallowing "|| true" inside the substitution
  # would mask Mustang's exit code, and bare "rc=$?" under set -e dies
  # on non-zero before it can read the status.
  if report=$(java -jar /opt/mustang.jar --action validate --source "$pdf" 2>&1 < /dev/null); then
    rc=0
  else
    rc=$?
  fi
  if [ "$rc" -eq 0 ]; then
    valid=true
  else
    valid=false
  fi
  # Mustang's XML validation report can exceed ARG_MAX when passed
  # as --arg, so --rawfile keeps the jq invocation within the kernel
  # argv limit even on the most verbose fixtures.
  report_file="$STAGE/report-$(printf '%05d' "$count").txt"
  printf '%s' "$report" > "$report_file"
  jq -n --arg file "$base" --argjson valid "$valid" --rawfile report "$report_file" \
    '{file: $file, valid: $valid, report: $report}' \
    > "$STAGE/$(printf '%05d' "$count").json"
  count=$((count + 1))
done < <(find "$PDFS_DIR" -maxdepth 1 -type f -iname '*.pdf' -print0 | sort -z)

# Use find + xargs over a glob so corpora with hundreds of fixtures
# do not blow ARG_MAX on the final aggregation.
find "$STAGE" -maxdepth 1 -name '*.json' -print0 \
  | sort -z \
  | xargs -0 jq -s '{jobs: .}' > "$OUT_FILE"
echo "  mustang: ${count} jobs written to ${OUT_FILE}"
