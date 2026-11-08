#!/usr/bin/env bash
# poi/ppt/download.sh: install the LibreOffice reference PDFs of the poi/ppt
# corpus.
#
# The corpus downloads nothing here. Its fixtures are the documents `make
# office-fetch SOURCES=poi FORMATS=ppt` puts in the testdata, and the PDFs it
# provisions are their LibreOffice references, which `make office-references
# SOURCES=poi FORMATS=ppt` builds into the testdata's references/ directory with
# the Gotenberg image versions.env pins. They are read from the testdata
# volume the image mounts at /output, not baked into the image, and the
# provisioning image derives the diff goldens from them (see
# lib/install-references.sh).
#
# Usage: poi/ppt/download.sh <output_pdfs_dir>
set -euo pipefail

exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../lib/install-references.sh" "$1" poi/ppt
