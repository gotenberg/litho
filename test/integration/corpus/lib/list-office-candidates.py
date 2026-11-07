#!/usr/bin/env python3
"""List the LibreOffice test documents an Office corpus scores.

Walks the test data directories of a LibreOffice core checkout at
LIBREOFFICE_CORE_COMMIT and writes, for one corpus (docx, odt, xlsx, ods or
csv), the documents the diff feature of test/integration/cmd/corpus
converts: documents.json, the fixture list ({"id": "<dir>/<file>"} each,
the path under the corpus testdata's sources/), and files.sha256, one
"<sha256>  <core path>" line per document plus the licence, which
fetch-office.sh downloads. <dir> is the Writer test directory
(sw/qa/extras/<dir>/data), or the module of a Calc or chart test directory
(sc for sc/qa/unit/data/<ext>, chart2 for chart2/qa/extras/data/<ext>,
each read for files of its <ext> alone).

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
always qualifies. Every kind is at most MAX_BYTES, its name is plain ASCII
(letters, digits, dot, underscore, hyphen), so every shell and form field
carries it intact, and Gotenberg's LibreOffice route converts it (REFUSED
lists the ones it does not).

The selection is deterministic: the qualifying files of each directory are
sorted by name and the first CAPS[corpus][dir] kept.

Usage: list-office-candidates.py <core_checkout> <corpus> <corpus_dir>
Run with python3 -I. Standard library only.
"""

import hashlib
import json
import os
import re
import sys
import zipfile

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
}

# REFUSED are the documents Gotenberg's LibreOffice route refuses, which have
# no reference to score litho against.
REFUSED = {
    # Malformed on purpose: the route answers 400, "LibreOffice could not
    # read the document".
    "sw/qa/extras/ooxmlimport/data/math-malformed_xml.docx",
}

# LICENSE is the licence fetched beside the documents.
LICENSE = "COPYING.MPL"

PLAIN_NAME = re.compile(r"^[A-Za-z0-9._-]+$")

DOCX_DENIED_DIRS = ("word/embeddings/", "word/charts/", "word/diagrams/", "word/activeX/")
DOCX_DENIED_PICTURES = (".emf", ".wmf", ".svm", ".tif", ".tiff")
ODT_DENIED_PICTURES = (".wmf", ".emf", ".svm")
XLSX_DENIED_DIRS = ("xl/embeddings/", "xl/activex/")
XLSX_DENIED_PICTURES = DOCX_DENIED_PICTURES
CHART_MEDIA_TYPE = "application/vnd.oasis.opendocument.chart"

# FILE_ENTRY and ATTR read the manifest's file entries, whose attributes come
# in any order.
FILE_ENTRY = re.compile(rb"<manifest:file-entry\b[^>]*>")
ATTR = re.compile(rb'manifest:(full-path|media-type)="([^"]*)"')

# EMBEDDED_TYPE finds the media type of each document a flat ODF file
# embeds, an office:document inside a draw:object; the root element carries
# the spreadsheet's own type and is skipped.
EMBEDDED_TYPE = re.compile(rb'<office:document\b[^>]*office:mimetype="([^"]*)"')

# MAX_CONTENT_BYTES bounds the content.xml read to look for the text body. A
# document of at most MAX_BYTES compressed rarely inflates past it; one that
# does is left out rather than read whole.
MAX_CONTENT_BYTES = 64 << 20


def docx_qualifies(path):
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
    except (zipfile.BadZipFile, OSError):
        return False
    if "word/document.xml" not in names:
        return False
    for n in names:
        low = n.lower()
        if low.startswith(tuple(d.lower() for d in DOCX_DENIED_DIRS)):
            return False
        if low.endswith("vbaproject.bin") or low.endswith(DOCX_DENIED_PICTURES):
            return False
    return True


def odt_qualifies(path):
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            for n in names:
                if n.startswith("Object") or n.startswith("Basic/"):
                    return False
                if n.lower().endswith(ODT_DENIED_PICTURES):
                    return False
            if "content.xml" not in names:
                return False
            info = z.getinfo("content.xml")
            if info.file_size > MAX_CONTENT_BYTES:
                return False
            content = z.read("content.xml")
    except (zipfile.BadZipFile, OSError, RuntimeError, NotImplementedError):
        return False
    return b"<office:text" in content


def xlsx_qualifies(path):
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
    except (zipfile.BadZipFile, OSError):
        return False
    if "xl/workbook.xml" not in names:
        return False
    for n in names:
        low = n.lower()
        if low.startswith(XLSX_DENIED_DIRS) or low == "xl/vbaproject.bin":
            return False
        if low.endswith(XLSX_DENIED_PICTURES):
            return False
    return True


def ods_qualifies(path):
    if path.endswith(".fods"):
        return fods_qualifies(path)
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            objects = set()
            for n in names:
                if n.startswith("Basic/") or n.lower().endswith(ODT_DENIED_PICTURES):
                    return False
                top, sep, _ = n.partition("/")
                # ObjectReplacements/ holds the pictures LibreOffice shows
                # in place of the objects, not objects of its own.
                if sep and top.startswith("Object") and top != "ObjectReplacements":
                    objects.add(top + "/")
            if "content.xml" not in names:
                return False
            if z.getinfo("content.xml").file_size > MAX_CONTENT_BYTES:
                return False
            content = z.read("content.xml")
            types = {}
            if objects:
                if "META-INF/manifest.xml" not in names:
                    return False
                for entry in FILE_ENTRY.findall(z.read("META-INF/manifest.xml")):
                    attrs = dict(ATTR.findall(entry))
                    types[attrs.get(b"full-path", b"").decode("utf-8", "replace")] = attrs.get(b"media-type", b"")
    except (zipfile.BadZipFile, OSError, RuntimeError, NotImplementedError):
        return False
    if any(types.get(o) != CHART_MEDIA_TYPE.encode() for o in objects):
        return False
    return b"<office:spreadsheet" in content


def fods_qualifies(path):
    # The size cap holds a flat file to MAX_BYTES, read whole.
    with open(path, "rb") as f:
        content = f.read()
    if b"<office:spreadsheet" not in content:
        return False
    embedded = EMBEDDED_TYPE.findall(content)[1:]
    return all(t == CHART_MEDIA_TYPE.encode() for t in embedded)


def csv_qualifies(_path):
    return True


# QUALIFIES maps a corpus to its qualifying test and the extensions it
# reads.
QUALIFIES = {
    "docx": (docx_qualifies, (".docx",)),
    "odt": (odt_qualifies, (".odt",)),
    "xlsx": (xlsx_qualifies, (".xlsx",)),
    "ods": (ods_qualifies, (".ods", ".fods")),
    "csv": (csv_qualifies, (".csv",)),
}


def dir_extensions(d, exts):
    """Return the extensions read in the core directory d: a Calc or chart
    test directory, named for its format, gives its own extension alone, so
    a fixture ID's extension names the directory (the flat file
    sc/qa/unit/data/ods holds is left to sc/qa/unit/data/fods)."""
    if d.split("/")[0] == "sw":
        return exts
    return ("." + d.rsplit("/", 1)[1],)


def short_dir(d):
    """Return the <dir> of the fixture IDs of the core directory d."""
    parts = d.split("/")
    if parts[0] == "sw":
        return parts[3]
    return parts[0]


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def candidate(full, name, exts, d):
    p = os.path.join(full, name)
    return (
        name.endswith(exts)
        and PLAIN_NAME.match(name)
        and d + "/" + name not in REFUSED
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
        sys.exit("usage: list-office-candidates.py <core_checkout> <%s> <corpus_dir>" % "|".join(CAPS))
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
            hashes.append((d + "/" + name, sha256(os.path.join(full, name))))
        counts.append((d, len(kept), cap, qualifying(full, names, exts, qualifies, d)))
    hashes.append((LICENSE, sha256(os.path.join(core, LICENSE))))
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
