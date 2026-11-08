#!/usr/bin/env bash
# poi/fetch.sh: download the documents of a poi corpus from apache/poi at the
# commit POI_COMMIT pins (see lib/fetch-github.sh).
#
# Usage: poi/fetch.sh <corpus_dir> <out_dir>
set -euo pipefail

exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/fetch-github.sh" apache/poi POI_COMMIT "$@"
