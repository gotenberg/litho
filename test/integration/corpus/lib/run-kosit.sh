#!/usr/bin/env bash
# run-kosit.sh: validate every PDF's embedded CII XML with KoSIT and
# emit one JSON report per corpus run.
#
# KoSIT operates on XML, not PDF, so the script extracts the CII
# payload via Mustang (which is already in the image for the
# facturx-validity feature) before each KoSIT invocation. The
# XRechnung scenarios bundle that ships with KoSIT routes documents
# to either XRechnung-specific Schematron, generic EN 16931 CII
# Schematron, or "no scenario applicable" based on the document's
# CustomizationID; the three-way verdict propagates into the JSON so
# the Go feature can score, fail, or skip each fixture distinctly.
#
# Output shape mirrors lib/run-mustang.sh enough that the scoreboard's
# per-job iteration code stays uniform:
#
#   {
#     "jobs": [
#       {"file": "<basename>.pdf", "verdict": "pass", "report": "..."},
#       {"file": "<basename>.pdf", "verdict": "fail", "report": "...",
#        "rules": ["BR-DE-1", "..."]},
#       {"file": "<basename>.pdf", "verdict": "skip",
#        "reason": "no scenario applicable"}
#     ]
#   }
#
# Usage: run-kosit.sh <pdfs_dir> <out_file>
set -euo pipefail

PDFS_DIR="$1"
OUT_FILE="$2"

if [ ! -f /opt/kosit.jar ]; then
  echo "ERROR: /opt/kosit.jar missing from corpus image" >&2
  exit 1
fi
if [ ! -f /opt/mustang.jar ]; then
  echo "ERROR: /opt/mustang.jar missing from corpus image" >&2
  exit 1
fi
SCENARIOS=/opt/kosit-config/scenarios.xml
if [ ! -f "$SCENARIOS" ]; then
  echo "ERROR: KoSIT scenarios.xml missing at $SCENARIOS" >&2
  exit 1
fi

mkdir -p "$(dirname "$OUT_FILE")"

STAGE=$(mktemp -d -t kosit-jobs.XXXXXX)
trap 'rm -rf "$STAGE"' EXIT

count=0
while IFS= read -r -d '' pdf; do
  base=$(basename "$pdf")
  work="$STAGE/work-$count"
  mkdir -p "$work"

  # Extract the embedded CII XML. Mustang writes to factur-x.xml in
  # the working directory; route stderr/stdout to /dev/null so the
  # script's loop output stays readable.
  if ! (cd "$work" && java -jar /opt/mustang.jar --action extract --source "$pdf" > /dev/null 2>&1); then
    jq -n --arg file "$base" \
      '{file: $file, verdict: "skip", reason: "no embedded CII found"}' \
      > "$STAGE/$(printf '%05d' "$count").json"
    count=$((count + 1))
    continue
  fi

  xml=""
  for candidate in factur-x.xml zugferd-invoice.xml ZUGFeRD-invoice.xml; do
    if [ -f "$work/$candidate" ]; then
      xml="$work/$candidate"
      break
    fi
  done
  if [ -z "$xml" ]; then
    jq -n --arg file "$base" \
      '{file: $file, verdict: "skip", reason: "extracted XML not found"}' \
      > "$STAGE/$(printf '%05d' "$count").json"
    count=$((count + 1))
    continue
  fi

  # KoSIT writes its XML report into the supplied output directory.
  # The exit code reflects "the validator ran"; verdict and applicable
  # scenarios live inside the report XML, so the script reads those
  # markers post-hoc rather than relying on the exit code alone.
  report_dir="$work/kosit"
  mkdir -p "$report_dir"
  if report=$(java -jar /opt/kosit.jar \
    --scenarios "$SCENARIOS" \
    --output "$report_dir" \
    "$xml" 2>&1); then
    rc=0
  else
    rc=$?
  fi

  report_xml=$(find "$report_dir" -maxdepth 2 -name '*.xml' | head -n 1)
  report_body=""
  if [ -n "$report_xml" ]; then
    report_body=$(cat "$report_xml")
  else
    report_body="$report"
  fi

  # Three-way detection markers. KoSIT's report shape varies across
  # releases; the patterns below cover the published 1.x reports:
  # rejected scenario block, accepted scenario block, or a top-level
  # marker indicating no scenario matched.
  if echo "$report_body" | grep -qiE '<acceptedBy>\s*</acceptedBy>|no scenario matched|no applicable scenario'; then
    verdict="skip"
    reason="no scenario applicable"
    jq -n --arg file "$base" --arg verdict "$verdict" --arg reason "$reason" --arg report "$report_body" \
      '{file: $file, verdict: $verdict, reason: $reason, report: $report}' \
      > "$STAGE/$(printf '%05d' "$count").json"
  else
    # Extract rejected rule IDs from failed assert messages. Keep
    # them inline as a small JSON array so the Go feature can list
    # them in the mismatch payload without re-parsing the report.
    rules=$(echo "$report_body" \
      | grep -oE 'id="[A-Z0-9_\-]+"' \
      | sed -E 's/^id="(.*)"$/\1/' \
      | sort -u \
      | jq -R . \
      | jq -s .)
    if [ "$rc" -eq 0 ]; then
      verdict="pass"
    else
      verdict="fail"
    fi
    jq -n --arg file "$base" --arg verdict "$verdict" --arg report "$report_body" --argjson rules "$rules" \
      '{file: $file, verdict: $verdict, report: $report, rules: $rules}' \
      > "$STAGE/$(printf '%05d' "$count").json"
  fi

  count=$((count + 1))
done < <(find "$PDFS_DIR" -maxdepth 1 -type f -iname '*.pdf' -print0 | sort -z)

jq -s '{jobs: .}' "$STAGE"/*.json > "$OUT_FILE"
echo "  kosit: ${count} jobs written to ${OUT_FILE}"
