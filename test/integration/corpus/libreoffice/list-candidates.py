#!/usr/bin/env python3
"""List the LibreOffice test documents an Office corpus scores.

Walks the test data directories of a LibreOffice core checkout at
LIBREOFFICE_CORE_COMMIT and writes, for one corpus (docx, odt, xlsx, ods,
csv, pptx or odp), the documents the diff feature of test/integration/cmd/corpus
converts: documents.json, the fixture list ({"id": "<dir>/<file>"} each,
the path under the corpus testdata's sources/), and files.sha256, one
"<sha256>  <core path>" line per document plus the licence, which
libreoffice/fetch.sh downloads. <dir> is the Writer test directory
(sw/qa/extras/<dir>/data), or the module of a Calc, Impress, DrawingML or
chart test directory (sc for sc/qa/unit/data/<ext>, sd for
sd/qa/unit/data/<ext>, chart2 for chart2/qa/extras/data/<ext>, oox for
oox/qa/unit/data), or sd-data for the mixed sd/qa/unit/data itself. A
directory named for a format is read for files of that format alone (the
OpenDocument presentation directory for its flat files too); any other for
the extensions the corpus reads.

A .docx qualifies when it is a zip package holding word/document.xml and no
part under word/embeddings/, word/charts/, word/diagrams/ or word/activeX/,
no vbaProject.bin, and no EMF, WMF, SVM or TIFF picture. A .odt qualifies
when it is a zip package whose content.xml holds an office:text body, with
no entry under Object*/ or Basic/ and no WMF, EMF or SVM picture. A .xlsx
qualifies when it is a zip package holding xl/workbook.xml and no part under
xl/embeddings/ or xl/activeX/, no xl/vbaProject.bin, and no EMF, WMF, SVM or
TIFF picture; charts are kept. A .ods qualifies when it is a zip package
whose content.xml holds an office:spreadsheet body, with no entry under
Basic/, no WMF, EMF or SVM picture, and every Object*/ directory a chart by
its manifest media type; a .fods when the flat file holds an
office:spreadsheet body and every document it embeds is a chart. A .csv
always qualifies. A .pptx qualifies when it is a zip package holding
ppt/presentation.xml and no part under ppt/activeX/, no ppt/vbaProject.bin,
no EMF, WMF, SVM or TIFF picture and no audio or video media, and whose
ppt/embeddings/ parts, if any, are the data workbooks of its charts alone
(no relationship but a chart's targets them): charts are kept, embedded
objects are not. A .odp qualifies as a .ods does with an
office:presentation body, and without audio or video media; a .fodp as a
.fods does. Every kind is at most MAX_BYTES, its name is plain ASCII
(letters, digits, dot, underscore, hyphen), so every shell and form field
carries it intact, Gotenberg's LibreOffice route converts it (REFUSED lists
the ones it does not), and the diff feature can score its reference
(UNSCORABLE lists the ones it cannot).

The selection is deterministic: the qualifying files of each directory are
sorted by name and the first CAPS[corpus][dir] kept.

Usage: libreoffice/list-candidates.py <core_checkout> <format> <corpus_dir>
Run with python3 -I. Standard library only.
"""

import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "lib"))
import office_qualify as oq  # noqa: E402

# MAX_BYTES bounds a candidate document: 150 KiB keeps the corpus a few tens
# of megabytes and a run within minutes, and leaves out the large documents
# whose size is mostly pictures and embedded fonts.
MAX_BYTES = 150 * 1024

# CAPS bounds the documents each directory contributes, so the export tests,
# which outnumber the rest, do not crowd out the import and layout tests.
CAPS = {
    "docx": {
        "sw/qa/extras/ooxmlexport/data": 250,
        "sw/qa/extras/ooxmlimport/data": 60,
        "sw/qa/extras/layout/data": 40,
    },
    "odt": {
        "sw/qa/extras/odfexport/data": 80,
        "sw/qa/extras/odfimport/data": 40,
        "sw/qa/extras/layout/data": 30,
    },
    "xlsx": {
        "sc/qa/unit/data/xlsx": 220,
        "chart2/qa/extras/data/xlsx": 60,
    },
    "ods": {
        "sc/qa/unit/data/ods": 200,
        "sc/qa/unit/data/fods": 16,
        "chart2/qa/extras/data/ods": 40,
    },
    "csv": {
        "sc/qa/unit/data/csv": 9,
    },
    "pptx": {
        "sd/qa/unit/data/pptx": 200,
        "oox/qa/unit/data": 61,
        "chart2/qa/extras/data/pptx": 38,
        "sd/qa/unit/data": 20,
    },
    "odp": {
        "sd/qa/unit/data/odp": 150,
        "sd/qa/unit/data": 20,
    },
}

# DIR_EXTENSIONS lists the directories named for a format that hold another
# extension of the corpus's too: Impress keeps its flat presentations beside
# the packaged ones, where Calc gives them a directory of their own.
DIR_EXTENSIONS = {
    "sd/qa/unit/data/odp": (".odp", ".fodp"),
}

# MIXED_DIRS are the module test data directories that hold documents of
# several formats beside the format directories under them; their fixture
# IDs take "<module>-data", so an ID never names two files.
MIXED_DIRS = ("sd/qa/unit/data",)

# REFUSED are the documents Gotenberg's LibreOffice route refuses, which have
# no reference to score litho against.
REFUSED = {
    # Malformed on purpose: the route answers 400, "LibreOffice could not
    # read the document".
    "sw/qa/extras/ooxmlimport/data/math-malformed_xml.docx",
}

# UNSCORABLE are documents the route converts whose reference the diff
# feature cannot score, left out with the reason.
UNSCORABLE = {
    # A slide of 500 by 500 cm, Impress's largest: drawn at 72 dpi it is
    # 14174 pixels square, four times the render budget (pdf.Limits'
    # MaxRenderPixels, 50 million) the cell draws a page within.
    "sd/qa/unit/data/odp/canvas-slide.odp",
}

# LICENSE is the licence fetched beside the documents.
LICENSE = "COPYING.MPL"

# QUALIFIES maps a corpus to its qualifying test and the extensions it
# reads.
QUALIFIES = {
    "docx": (oq.docx_qualifies, (".docx",)),
    "odt": (oq.odt_qualifies, (".odt",)),
    "xlsx": (oq.xlsx_qualifies, (".xlsx",)),
    "ods": (oq.ods_qualifies, (".ods", ".fods")),
    "csv": (oq.csv_qualifies, (".csv",)),
    "pptx": (oq.pptx_qualifies, (".pptx",)),
    "odp": (oq.odp_qualifies, (".odp", ".fodp")),
}


def dir_extensions(d, exts):
    """Return the extensions read in the core directory d: a test directory
    named for a format gives that extension alone, so a fixture ID's
    extension names the directory (the flat file sc/qa/unit/data/ods holds
    is left to sc/qa/unit/data/fods, the .odp files of sd/qa/unit/data/pptx
    to the odp corpus), unless DIR_EXTENSIONS lists it; any other directory,
    a Writer test's or a mixed one, gives the corpus's extensions."""
    if d in DIR_EXTENSIONS:
        return DIR_EXTENSIONS[d]
    own = "." + d.rsplit("/", 1)[1]
    if own in exts:
        return (own,)
    return exts


def short_dir(d):
    """Return the <dir> of the fixture IDs of the core directory d."""
    parts = d.split("/")
    if parts[0] == "sw":
        return parts[3]
    if d in MIXED_DIRS:
        return parts[0] + "-data"
    return parts[0]


def candidate(full, name, exts, d):
    p = os.path.join(full, name)
    return (
        name.endswith(exts)
        and oq.PLAIN_NAME.match(name)
        and d + "/" + name not in REFUSED
        and d + "/" + name not in UNSCORABLE
        and os.path.isfile(p)
        and os.path.getsize(p) <= MAX_BYTES
    )


def qualifying(full, names, exts, qualifies, d):
    """Count the directory's documents and those that qualify, uncapped."""
    docs = [n for n in names if n.endswith(exts) and os.path.isfile(os.path.join(full, n))]
    ok = [n for n in docs if candidate(full, n, exts, d) and qualifies(os.path.join(full, n))]
    return len(docs), len(ok)


def main():
    if len(sys.argv) != 4 or sys.argv[2] not in CAPS:
        sys.exit("usage: list-candidates.py <core_checkout> <%s> <corpus_dir>" % "|".join(CAPS))
    core, corpus, out = sys.argv[1], sys.argv[2], sys.argv[3]
    qualifies, corpus_exts = QUALIFIES[corpus]
    documents, hashes, counts = [], [], []
    for d, cap in CAPS[corpus].items():
        exts = dir_extensions(d, corpus_exts)
        full = os.path.join(core, d)
        names = sorted(os.listdir(full))
        kept = []
        for name in names:
            if not candidate(full, name, exts, d) or not qualifies(os.path.join(full, name)):
                continue
            kept.append(name)
            if len(kept) == cap:
                break
        short = short_dir(d)
        for name in kept:
            documents.append({"id": short + "/" + name})
            hashes.append((d + "/" + name, oq.sha256(os.path.join(full, name))))
        counts.append((d, len(kept), cap, qualifying(full, names, exts, qualifies, d)))
    hashes.append((LICENSE, oq.sha256(os.path.join(core, LICENSE))))
    with open(os.path.join(out, "documents.json"), "w", encoding="utf-8") as f:
        json.dump(documents, f, indent=2)
        f.write("\n")
    with open(os.path.join(out, "files.sha256"), "w", encoding="utf-8") as f:
        for path, h in hashes:
            f.write("%s  %s\n" % (h, path))
    for d, n, cap, (total, ok) in counts:
        print("%s: %d of at most %d (%d documents, %d qualifying)" % (d, n, cap, total, ok), file=sys.stderr)


if __name__ == "__main__":
    main()
