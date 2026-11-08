#!/usr/bin/env bash
# odp/download.sh: install the LibreOffice reference PDFs of the odp corpus.
#
# The odp corpus downloads nothing here. Its fixtures are the committed
# sources/ and the LibreOffice test documents `make odp-fetch` puts in the
# testdata, and the PDFs it provisions are their LibreOffice references,
# which `make odp-libreoffice` builds into the testdata's libreoffice/
# directory with the Gotenberg image versions.env pins. They are read from the
# testdata volume the image mounts at /output, not baked into the image, and
# the provisioning image derives the diff goldens from them (see
# lib/install-references.sh).
#
# Usage: odp/download.sh <output_pdfs_dir>
set -euo pipefail

exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/install-references.sh" "$1" odp
