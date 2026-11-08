#!/usr/bin/env bash
# openxmlsdk/fetch.sh: download the documents of a openxmlsdk corpus from dotnet/Open-XML-SDK at the
# commit OPENXMLSDK_COMMIT pins (see lib/fetch-github.sh).
#
# Usage: openxmlsdk/fetch.sh <corpus_dir> <out_dir>
set -euo pipefail

exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/fetch-github.sh" dotnet/Open-XML-SDK OPENXMLSDK_COMMIT "$@"
