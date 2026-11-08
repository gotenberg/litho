#!/usr/bin/env python3
"""List the Govdocs1 Office documents a govdocs Office corpus scores.

Reads the thread archives GOVDOCS_THREADS pins (the threads the govdocs/pdf
corpus samples) through HTTP range requests and writes, for one format (doc,
xls or ppt), the documents the diff feature of test/integration/cmd/corpus
converts: documents.json ({"id", "path"} each, the ID "<thread>/<file>",
the path "<thread>.zip/<entry>") and files.sha256 ("<sha256>  <path>" per
document), which govdocs/fetch.sh downloads. The candidates are the
archives' files of the format's extension of at most MAX_BYTES, taken in
the order of the SHA-256 of their path, so the choice stays put when the
caps change; each is downloaded, and kept when it qualifies as
lib/office_qualify.py decides, until CAPS[format] are kept.

Govdocs1 holds documents of United States government web sites, which
Digital Corpora redistributes for research; they are fetched for testing
and never committed.

Usage: govdocs/list-candidates.py <format> <corpus_dir>
Run with python3 -I. Standard library only.
"""

import hashlib
import os
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "lib"))
import office_qualify as oq  # noqa: E402
import remote_zip  # noqa: E402

BASE = "https://digitalcorpora.s3.amazonaws.com/corpora/files/govdocs1/zipfiles"

# MAX_BYTES bounds a candidate document: real documents run larger than the
# test suites' (the pinned threads' presentations average 3 MB), and 2 MiB
# keeps most word processing documents and workbooks and the lighter half of
# the presentations.
MAX_BYTES = 2 << 20

# CAPS bounds the documents of each format.
CAPS = {"doc": 100, "xls": 100, "ppt": 100}


def threads():
    pinned = os.environ.get("GOVDOCS_THREADS")
    if not pinned:
        with open(os.path.join(HERE, "..", "versions.env"), encoding="utf-8") as f:
            for line in f:
                if line.startswith("GOVDOCS_THREADS="):
                    pinned = line.split("=", 1)[1].strip()
    if not pinned:
        sys.exit("GOVDOCS_THREADS is not pinned")
    return pinned.split(",")


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in CAPS:
        sys.exit("usage: list-candidates.py <%s> <corpus_dir>" % "|".join(CAPS))
    fmt, out = sys.argv[1:]
    candidates = []
    archives = {}
    for t in threads():
        z = remote_zip.open_zip("%s/%s.zip" % (BASE, t))
        archives[t] = z
        for info in z.infolist():
            name = os.path.basename(info.filename)
            if name.lower().endswith("." + fmt) and 0 < info.file_size <= MAX_BYTES and oq.PLAIN_NAME.match(name):
                path = "%s.zip/%s" % (t, info.filename)
                candidates.append((hashlib.sha256(path.encode()).hexdigest(), t, info.filename, path))
    candidates.sort()
    kept, tried = [], 0
    with tempfile.TemporaryDirectory() as tmp:
        for _, t, entry, path in candidates:
            if len(kept) == CAPS[fmt]:
                break
            tried += 1
            local = os.path.join(tmp, os.path.basename(entry))
            with open(local, "wb") as f:
                f.write(archives[t].read(entry))
            if oq.QUALIFIES[fmt](local):
                kept.append(("%s/%s" % (t, os.path.basename(entry)), path, oq.sha256(local)))
            os.remove(local)
    kept.sort()
    oq.write_lists(out, kept, [])
    print("%s: %d of at most %d (%d candidates, %d tried)" % (fmt, len(kept), CAPS[fmt], len(candidates), tried), file=sys.stderr)


if __name__ == "__main__":
    main()
