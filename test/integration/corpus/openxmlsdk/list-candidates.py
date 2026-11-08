#!/usr/bin/env python3
"""List the Open XML SDK test documents an openxmlsdk corpus scores.

Walks the test data storage of an Open XML SDK checkout at OPENXMLSDK_COMMIT
(test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage) and
writes, for one format (docx, xlsx or pptx), the documents the diff feature
of test/integration/cmd/corpus converts: documents.json ({"id", "path"}
each, the ID the document's path under the storage made plain ASCII, the
path its path in the repository) and files.sha256 ("<sha256>  <path>" per
document, then the licence), which openxmlsdk/fetch.sh downloads. A document
qualifies as lib/office_qualify.py decides, is at most MAX_BYTES, is not
under Robustness/ (malformed on purpose), Gotenberg's LibreOffice route
converts it (REFUSED lists the ones it does not), and its bytes are kept
once: the storage holds several sets twice. The qualifying documents are sorted by ID
and the first CAPS[format] kept.

Usage: openxmlsdk/list-candidates.py <sdk_checkout> <format> <corpus_dir>
Run with python3 -I. Standard library only.
"""

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "lib"))
import office_qualify as oq  # noqa: E402

# MAX_BYTES bounds a candidate document, as the libreoffice corpora's.
MAX_BYTES = 150 * 1024

# CAPS bounds the documents of each format.
CAPS = {"docx": 250, "xlsx": 250, "pptx": 250}

# EXTENSIONS are the extensions each format reads: the templates convert as
# their documents do.
EXTENSIONS = {"docx": (".docx", ".dotx"), "xlsx": (".xlsx", ".xltx"), "pptx": (".pptx", ".potx")}

# REFUSED are the documents Gotenberg's LibreOffice route refuses ("could
# not read the document", HTTP 400), which have no reference to score litho
# against.
REFUSED = {
    "v2FxTestFiles/spreadsheet/O12 templates/ProjectStatusReport_TP10094814.xltx",
}

SUBDIR = "test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage"
LICENCE = ("LICENSE",)


def main():
    if len(sys.argv) != 4 or sys.argv[2] not in CAPS:
        sys.exit("usage: list-candidates.py <sdk_checkout> <%s> <corpus_dir>" % "|".join(CAPS))
    checkout, fmt, out = sys.argv[1:]
    kept, candidates, qualifying = oq.select(checkout, SUBDIR, fmt, EXTENSIONS[fmt], CAPS[fmt], MAX_BYTES, lambda rel: rel.startswith("Robustness/") or rel in REFUSED)
    oq.write_lists(out, kept, [(p, oq.sha256(os.path.join(checkout, p))) for p in LICENCE])
    print("%s: %d of at most %d (%d candidates, %d qualifying)" % (fmt, len(kept), CAPS[fmt], candidates, qualifying), file=sys.stderr)


if __name__ == "__main__":
    main()
