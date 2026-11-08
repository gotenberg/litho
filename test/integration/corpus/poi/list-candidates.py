#!/usr/bin/env python3
"""List the Apache POI test documents a poi corpus scores.

Walks test-data/ of an Apache POI checkout at POI_COMMIT and writes, for one
format (docx, xlsx, pptx, doc, xls or ppt), the documents the diff feature
of test/integration/cmd/corpus converts: documents.json ({"id", "path"}
each, the ID the document's path under test-data/, the path its path in the
repository) and files.sha256 ("<sha256>  <path>" per document, then the
licence files), which poi/fetch.sh downloads. A document qualifies as
lib/office_qualify.py decides, is at most MAX_BYTES, is not a file POI's own
test-data/poi-integration-exceptions.csv lists (the encrypted, malformed and
fuzzed files POI expects to fail on), Gotenberg's LibreOffice route converts
it (REFUSED lists the ones it does not), and its bytes are kept once. The
qualifying documents are sorted by ID and the first CAPS[format] kept.

Usage: poi/list-candidates.py <poi_checkout> <format> <corpus_dir>
Run with python3 -I. Standard library only.
"""

import csv
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "lib"))
import office_qualify as oq  # noqa: E402

# MAX_BYTES bounds a candidate document, as the libreoffice corpora's: 150
# KiB keeps the corpus a few tens of megabytes and a run within minutes.
MAX_BYTES = 150 * 1024

# CAPS bounds the documents of each format: the spreadsheets, which
# outnumber the rest, keep 200.
CAPS = {"docx": 200, "xlsx": 200, "pptx": 200, "doc": 200, "xls": 200, "ppt": 200}

# EXTENSIONS are the extensions each format reads: the templates convert as
# their documents do.
EXTENSIONS = {
    "docx": (".docx", ".dotx"),
    "xlsx": (".xlsx", ".xltx"),
    "pptx": (".pptx", ".potx"),
    "doc": (".doc",),
    "xls": (".xls",),
    "ppt": (".ppt",),
}

# REFUSED are the documents Gotenberg's LibreOffice route refuses ("could
# not read the document", HTTP 400), which have no reference to score litho
# against.
REFUSED = {
    "document/ExternalEntityInText.docx",
    "slideshow/7ffe3cabb976ebc593dfe2f9461bdeac980a626c.ppt",
    "slideshow/backgrounds.ppt",
    "slideshow/pictures.ppt",
}

SUBDIR = "test-data"
EXCEPTIONS = "test-data/poi-integration-exceptions.csv"
LICENCE = ("legal/LICENSE", "legal/NOTICE")


def main():
    if len(sys.argv) != 4 or sys.argv[2] not in CAPS:
        sys.exit("usage: list-candidates.py <poi_checkout> <%s> <corpus_dir>" % "|".join(CAPS))
    checkout, fmt, out = sys.argv[1:]
    with open(os.path.join(checkout, EXCEPTIONS), encoding="utf-8-sig") as f:
        excluded = {row["File"] for row in csv.DictReader(f)}
    kept, candidates, qualifying = oq.select(checkout, SUBDIR, fmt, EXTENSIONS[fmt], CAPS[fmt], MAX_BYTES, lambda rel: rel in excluded or rel in REFUSED)
    oq.write_lists(out, kept, [(p, oq.sha256(os.path.join(checkout, p))) for p in LICENCE])
    print("%s: %d of at most %d (%d candidates, %d qualifying)" % (fmt, len(kept), CAPS[fmt], candidates, qualifying), file=sys.stderr)


if __name__ == "__main__":
    main()
