#!/usr/bin/env python3
"""List the LibreOffice test documents the docx or odt corpus scores.

Walks the test data directories of a LibreOffice core checkout at
LIBREOFFICE_CORE_COMMIT and writes, for one corpus, the documents the diff
feature of test/integration/cmd/corpus converts: documents.json, the fixture
list ({"id": "<dir>/<file>"} each, the path under the corpus testdata's
sources/), and files.sha256, one "<sha256>  <core path>" line per document
plus the licence, which fetch-office.sh downloads.

A .docx qualifies when it is a zip package holding word/document.xml and no
part under word/embeddings/, word/charts/, word/diagrams/ or word/activeX/,
no vbaProject.bin, and no EMF, WMF, SVM or TIFF picture. A .odt qualifies
when it is a zip package whose content.xml holds an office:text body, with
no entry under Object*/ or Basic/ and no WMF, EMF or SVM picture. Either way
the file is at most MAX_BYTES, its name is plain ASCII (letters, digits,
dot, underscore, hyphen), so every shell and form field carries it intact,
and Gotenberg's LibreOffice route converts it (REFUSED lists the ones it
does not).

The selection is deterministic: the qualifying files of each directory are
sorted by name and the first CAPS[corpus][dir] kept.

Usage: list-office-candidates.py <core_checkout> <docx|odt> <corpus_dir>
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


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    if len(sys.argv) != 4 or sys.argv[2] not in CAPS:
        sys.exit("usage: list-office-candidates.py <core_checkout> <docx|odt> <corpus_dir>")
    core, corpus, out = sys.argv[1], sys.argv[2], sys.argv[3]
    qualifies = docx_qualifies if corpus == "docx" else odt_qualifies
    ext = "." + corpus
    documents, hashes, counts = [], [], []
    for d, cap in CAPS[corpus].items():
        full = os.path.join(core, d)
        names = sorted(os.listdir(full))
        kept = []
        for name in names:
            p = os.path.join(full, name)
            if not name.endswith(ext) or not PLAIN_NAME.match(name):
                continue
            if d + "/" + name in REFUSED:
                continue
            if not os.path.isfile(p) or os.path.getsize(p) > MAX_BYTES:
                continue
            if not qualifies(p):
                continue
            kept.append(name)
            if len(kept) == cap:
                break
        short = d.split("/")[3]
        for name in kept:
            documents.append({"id": short + "/" + name})
            hashes.append((d + "/" + name, sha256(os.path.join(full, name))))
        counts.append((d, len(kept), cap))
    hashes.append((LICENSE, sha256(os.path.join(core, LICENSE))))
    with open(os.path.join(out, "documents.json"), "w", encoding="utf-8") as f:
        json.dump(documents, f, indent=2)
        f.write("\n")
    with open(os.path.join(out, "files.sha256"), "w", encoding="utf-8") as f:
        for path, h in hashes:
            f.write("%s  %s\n" % (h, path))
    for d, n, cap in counts:
        print("%s: %d of at most %d" % (d, n, cap), file=sys.stderr)


if __name__ == "__main__":
    main()
