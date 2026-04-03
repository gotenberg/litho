#!/usr/bin/env bash
# setup.sh — VeraPDF corpus generation entrypoint for the testcontainer.
#
# Clones the veraPDF test corpus, then runs the veraPDF CLI against each
# profile directory to produce JSON validation references.
# Output is written to /output (bind-mounted from host).
set -euo pipefail

OUTPUT_DIR="/output"

echo "==> Cloning veraPDF corpus..."
git clone --depth 1 https://github.com/veraPDF/veraPDF-corpus.git /tmp/verapdf-corpus

echo "==> Copying corpus PDFs..."
mkdir -p "$OUTPUT_DIR/references"

for dir in /tmp/verapdf-corpus/PDF_A-* /tmp/verapdf-corpus/PDF_UA-*; do
  [ -d "$dir" ] || continue
  dirname=$(basename "$dir")
  echo "  copying $dirname..."
  cp -r "$dir" "$OUTPUT_DIR/$dirname"
done

# Map directory names to veraPDF CLI flavour flags.
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

echo "==> Running veraPDF validation..."
for dir in "$OUTPUT_DIR"/PDF_A-* "$OUTPUT_DIR"/PDF_UA-*; do
  [ -d "$dir" ] || continue
  dirname=$(basename "$dir")
  flavour="${FLAVOURS[$dirname]:-}"
  if [ -z "$flavour" ]; then
    echo "  WARN: no flavour mapping for $dirname, skipping"
    continue
  fi

  echo "  validating $dirname (flavour=$flavour)..."
  verapdf -f "$flavour" -r --format json --maxfailuresdisplayed -1 "$dir" \
    > "$OUTPUT_DIR/references/$dirname.json" 2> /dev/null \
    || echo "  WARN: verapdf had errors for $dirname (exit $?)"
done

echo "==> Done."
