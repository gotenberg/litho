#!/usr/bin/env bash
# govdocs/fetch.sh: download the documents of a govdocs Office corpus out of
# the Govdocs1 thread archives, reading only their entries through HTTP
# range requests (see lib/remote_zip.py), hash-checked.
#
# Usage: govdocs/fetch.sh <corpus_dir> <out_dir>
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec python3 -I "$HERE/../lib/remote_zip.py" \
  https://digitalcorpora.s3.amazonaws.com/corpora/files/govdocs1/zipfiles "$@"
