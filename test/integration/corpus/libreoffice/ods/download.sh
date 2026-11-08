#!/usr/bin/env bash
# libreoffice/ods/download.sh: install the LibreOffice reference PDFs of the
# libreoffice/ods corpus.
#
# The corpus downloads nothing here. Its fixtures are the LibreOffice test
# documents `make office-fetch SOURCES=libreoffice FORMATS=ods` puts in the testdata, and the
# PDFs it provisions are their LibreOffice references, which `make
# office-references SOURCES=libreoffice FORMATS=ods` builds into the testdata's references/
# directory with the Gotenberg image versions.env pins. They are read from the
# testdata volume the image mounts at /output, not baked into the image, and
# the provisioning image derives the diff goldens from them (see
# lib/install-references.sh).
#
# Usage: libreoffice/ods/download.sh <output_pdfs_dir>
set -euo pipefail

exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../../lib/install-references.sh" "$1" libreoffice/ods
