#!/usr/bin/env bash
# offcrypto/testdata/soffice.sh: encrypt documents with the pinned Gotenberg
# image's LibreOffice, for the offcrypto round trips against a real writer.
#
# It builds soffice/Dockerfile (the image plus the Python UNO bridge) and
# saves, with the password foo, under <out_dir>:
#
#   - memo-blowfish.odt: ODF 1.1, Blowfish CFB, PBKDF2, SHA-1 start key.
#   - memo-aes-cbc.odt: ODF 1.3, AES-256-CBC, PBKDF2, SHA-256 start key.
#   - memo-gcm.odt: LibreOffice's default, AES-256-GCM and Argon2id over the
#     whole package (encrypted-package).
#   - page_1-lo.docx: LibreOffice's OOXML writer, Standard encryption.
#
# The sources are Gotenberg's memo.odt and page_1.docx (MIT), which fetch.sh
# downloads first. Salts and IVs are random, so the outputs are not pinned
# by hash. Nothing is committed.
#
# Usage: soffice.sh [out_dir], out_dir defaulting to
# test/integration/testdata/offcrypto/soffice.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA="$HERE/../../../../test/integration/testdata/offcrypto"
OUT="${1:-$DATA/soffice}"
VERSIONS="$HERE/../../../../test/integration/corpus/versions.env"
IMAGE="${GOTENBERG_IMAGE:-$(sed -n 's/^GOTENBERG_IMAGE=//p' "$VERSIONS")}"

"$HERE/fetch.sh" "$DATA"
SRC="$(mktemp -d)"
trap 'rm -rf "$SRC"' EXIT
cp "$DATA/gotenberg/test/integration/testdata/page_1.docx" "$DATA/gotenberg/test/corpus/testdata/office/memo.odt" "$SRC/"
mkdir -p "$OUT"
TAG="litho-offcrypto-soffice"
docker build -q --build-arg "GOTENBERG_IMAGE=$IMAGE" -t "$TAG" "$HERE/soffice" > /dev/null
docker run --rm -v "$SRC:/src:ro" -v "$OUT:/out" -v "$HERE/soffice:/script:ro" \
  --entrypoint python3 "$TAG" /script/encrypt.py \
  "/src/memo.odt,/out/memo-blowfish.odt,writer8,2,foo" \
  "/src/memo.odt,/out/memo-aes-cbc.odt,writer8,10,foo" \
  "/src/memo.odt,/out/memo-gcm.odt,writer8,,foo" \
  "/src/page_1.docx,/out/page_1-lo.docx,MS Word 2007 XML,,foo"
