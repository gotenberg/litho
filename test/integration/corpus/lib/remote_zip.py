"""Read single entries of a remote zip archive through HTTP range requests.

The govdocs Office corpora sample a few hundred documents out of the
Govdocs1 thread archives, 2 GB in all for the pinned threads: a RangeFile
reads an archive's central directory and the entries chosen, not the whole
archive. Used as a module by govdocs/list-candidates.py, and as a script by
govdocs/fetch.sh:

    python3 -I lib/remote_zip.py <base_url> <corpus_dir> <out_dir>

downloads each document <corpus_dir>/documents.json lists, its path
"<archive>/<entry>" read from <base_url>/<archive>, into <out_dir>/<id>,
refusing one whose SHA-256 is not the one <corpus_dir>/files.sha256 lists. A
document already on disk with that hash is kept. Standard library only.
"""

import hashlib
import io
import json
import os
import sys
import urllib.request
import zipfile

# CHUNK is the smallest range read: the central directory and a document
# take a few requests each rather than one per zipfile read.
CHUNK = 1 << 20
# RETRIES bounds the attempts at one range read.
RETRIES = 3


class RangeFile(io.RawIOBase):
    """A seekable, read-only view of a remote file over HTTP range requests."""

    def __init__(self, url):
        self.url, self.pos = url, 0
        with urllib.request.urlopen(urllib.request.Request(url, method="HEAD"), timeout=60) as r:
            self.size = int(r.headers["Content-Length"])

    def readable(self):
        return True

    def seekable(self):
        return True

    def tell(self):
        return self.pos

    def seek(self, offset, whence=0):
        self.pos = offset if whence == 0 else self.pos + offset if whence == 1 else self.size + offset
        return self.pos

    def readinto(self, buf):
        if self.pos >= self.size or not len(buf):
            return 0
        end = min(self.size, self.pos + len(buf)) - 1
        req = urllib.request.Request(self.url, headers={"Range": "bytes=%d-%d" % (self.pos, end)})
        for attempt in range(RETRIES):
            try:
                with urllib.request.urlopen(req, timeout=120) as r:
                    data = r.read()
                break
            except OSError:
                if attempt == RETRIES - 1:
                    raise
        buf[: len(data)] = data
        self.pos += len(data)
        return len(data)


def open_zip(url):
    """Return the zipfile.ZipFile of the remote archive at url."""
    return zipfile.ZipFile(io.BufferedReader(RangeFile(url), CHUNK))


def fetch(base, corpus, out):
    hashes = {}
    with open(os.path.join(corpus, "files.sha256"), encoding="utf-8") as f:
        for line in f:
            digest, _, path = line.rstrip("\n").partition("  ")
            hashes[path] = digest
    with open(os.path.join(corpus, "documents.json"), encoding="utf-8") as f:
        documents = json.load(f)
    archives = {}
    for doc in documents:
        archive, _, entry = doc["path"].partition("/")
        dst = os.path.join(out, doc["id"])
        if os.path.isfile(dst) and sha256_file(dst) == hashes[doc["path"]]:
            continue
        if archive not in archives:
            archives[archive] = open_zip(base + "/" + archive)
        data = archives[archive].read(entry)
        got = hashlib.sha256(data).hexdigest()
        if got != hashes[doc["path"]]:
            sys.exit("ERROR: %s hashes to %s, files.sha256 lists %s" % (doc["path"], got, hashes[doc["path"]]))
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        with open(dst + ".part", "wb") as f:
            f.write(data)
        os.replace(dst + ".part", dst)
    print("  %s: %d documents, hash-checked" % (os.path.relpath(corpus, os.path.dirname(os.path.dirname(corpus))), len(documents)))


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


if __name__ == "__main__":
    if len(sys.argv) != 4:
        sys.exit("usage: remote_zip.py <base_url> <corpus_dir> <out_dir>")
    fetch(*sys.argv[1:])
