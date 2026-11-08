"""Decide whether an Office document qualifies for a converter corpus.

Shared by the selectors of the Office corpora (libreoffice, poi,
openxmlsdk and govdocs list-candidates.py): each walks its own source and
asks this module whether a document is in scope. A document qualifies when
it is a well-formed package or compound file of its format, carries no
macros, ActiveX controls or embedded objects (charts aside), no picture of
a format litho does not decode (EMF, WMF, SVM, TIFF), and no audio or
video; a binary document must also be unencrypted. The rules are the
libreoffice corpus's, moved here unchanged, and those of the binary
formats beside them. Standard library only.
"""

import hashlib
import json
import os
import re
import struct
import zipfile

PLAIN_NAME = re.compile(r"^[A-Za-z0-9._-]+$")

DOCX_DENIED_DIRS = ("word/embeddings/", "word/charts/", "word/diagrams/", "word/activeX/")
DOCX_DENIED_PICTURES = (".emf", ".wmf", ".svm", ".tif", ".tiff")
ODT_DENIED_PICTURES = (".wmf", ".emf", ".svm")
XLSX_DENIED_DIRS = ("xl/embeddings/", "xl/activex/")
XLSX_DENIED_PICTURES = DOCX_DENIED_PICTURES
PPTX_DENIED_DIRS = ("ppt/activex/",)
PPTX_EMBEDDINGS = "ppt/embeddings/"
# CHART_RELS holds the relationships of the chart parts, whose embedded
# workbook is the chart's data (ECMA-376 Part 1, 21.2.2.63 externalData),
# which no viewer draws.
CHART_RELS = "ppt/charts/_rels/"
# EMBEDDING_TARGET finds a relationship that targets an embedded part.
EMBEDDING_TARGET = re.compile(rb'Target="[^"]*embeddings/', re.IGNORECASE)
# MEDIA_EXTENSIONS are the audio and video files a presentation plays, which
# a PDF of its slides cannot hold.
MEDIA_EXTENSIONS = (".mp4", ".mov", ".avi", ".wmv", ".mp3", ".wav", ".m4a")
CHART_MEDIA_TYPE = "application/vnd.oasis.opendocument.chart"

# FILE_ENTRY and ATTR read the manifest's file entries, whose attributes come
# in any order.
FILE_ENTRY = re.compile(rb"<manifest:file-entry\b[^>]*>")
ATTR = re.compile(rb'manifest:(full-path|media-type)="([^"]*)"')

# EMBEDDED_TYPE finds the media type of each document a flat ODF file
# embeds, an office:document inside a draw:object; the root element carries
# the spreadsheet's own type and is skipped.
EMBEDDED_TYPE = re.compile(rb'<office:document\b[^>]*office:mimetype="([^"]*)"')

# FLAT_MEDIA finds a picture of a denied format or a played medium a flat
# presentation links or embeds: a draw:plugin, or an xlink:href or
# draw:mime-type naming one.
FLAT_MEDIA = re.compile(
    rb'<draw:plugin\b|xlink:href="[^"]*\.(?:wmf|emf|svm|mp4|mov|avi|wmv|mp3|wav|m4a)"'
    rb'|draw:mime-type="(?:audio|video)/',
    re.IGNORECASE,
)

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
        return flat_qualifies(path, b"<office:spreadsheet")
    return package_qualifies(path, b"<office:spreadsheet", ODT_DENIED_PICTURES)


def odp_qualifies(path):
    if path.endswith(".fodp"):
        return flat_qualifies(path, b"<office:presentation") and not FLAT_MEDIA.search(flat_content(path))
    return package_qualifies(path, b"<office:presentation", ODT_DENIED_PICTURES + MEDIA_EXTENSIONS)


def package_qualifies(path, body, denied):
    """Report whether the OpenDocument package at path holds body in its
    content.xml, no macros, no file of a denied extension, and only charts
    as embedded objects."""
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            objects = set()
            for n in names:
                if n.startswith("Basic/") or n.lower().endswith(denied):
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
    return body in content


def flat_content(path):
    # The size cap holds a flat file to MAX_BYTES, read whole.
    with open(path, "rb") as f:
        return f.read()


def flat_qualifies(path, body):
    """Report whether the flat OpenDocument file at path holds body and
    embeds charts alone."""
    content = flat_content(path)
    if body not in content:
        return False
    embedded = EMBEDDED_TYPE.findall(content)[1:]
    return all(t == CHART_MEDIA_TYPE.encode() for t in embedded)


def csv_qualifies(_path):
    return True


def pptx_qualifies(path):
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            if "ppt/presentation.xml" not in names:
                return False
            embedded = False
            for n in names:
                low = n.lower()
                if low.startswith(PPTX_DENIED_DIRS) or low == "ppt/vbaproject.bin":
                    return False
                if low.endswith(XLSX_DENIED_PICTURES) or low.endswith(MEDIA_EXTENSIONS):
                    return False
                embedded = embedded or low.startswith(PPTX_EMBEDDINGS)
            if not embedded:
                return True
            for n in names:
                if not n.endswith(".rels") or n.startswith(CHART_RELS):
                    continue
                if z.getinfo(n).file_size > MAX_CONTENT_BYTES:
                    return False
                if EMBEDDING_TARGET.search(z.read(n)):
                    return False
    except (zipfile.BadZipFile, OSError, RuntimeError, NotImplementedError):
        return False
    return True


# The binary formats are compound files (MS-CFB): a FAT-chained sector
# file holding a tree of storages and streams. The reader below lists the
# tree and reads the head of a stream, which is all the checks need.
CFB_MAGIC = b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
# CFB_END marks the end of a sector chain (MS-CFB 2.1: ENDOFCHAIN and the
# special values above it).
CFB_END = 0xFFFFFFFA
# CFB_STORAGE, CFB_STREAM and CFB_ROOT are the object types of a directory
# entry (MS-CFB 2.6.1).
CFB_STORAGE, CFB_STREAM, CFB_ROOT = 1, 2, 5
# MAX_CFB_BYTES bounds a compound file read whole: the selectors' own size
# caps keep a candidate far below it.
MAX_CFB_BYTES = 64 << 20


class CompoundFile:
    """A compound file read whole, its directory listed (MS-CFB 2.2 to 2.6)."""

    def __init__(self, data):
        if len(data) < 512 or data[:8] != CFB_MAGIC:
            raise ValueError("not a compound file")
        self.data = data
        self.shift = struct.unpack_from("<H", data, 0x1E)[0]
        self.mini_shift = struct.unpack_from("<H", data, 0x20)[0]
        if self.shift not in (9, 12) or self.mini_shift != 6:
            raise ValueError("unsupported sector size")
        self.sector = 1 << self.shift
        self.count = (len(data) - 512) // self.sector + 1
        n_fat, first_dir = struct.unpack_from("<II", data, 0x2C)
        self.cutoff, first_mini, _, first_difat, n_difat = struct.unpack_from("<IIIII", data, 0x38)
        difat = list(struct.unpack_from("<109I", data, 0x4C))
        sid = first_difat
        for _ in range(n_difat):
            if sid >= CFB_END or sid >= self.count:
                break
            entries = struct.unpack_from("<%dI" % (self.sector // 4), self.sector_bytes(sid))
            difat.extend(entries[:-1])
            sid = entries[-1]
        self.fat = []
        for sid in difat[:n_fat]:
            if sid >= self.count:
                raise ValueError("FAT sector out of range")
            self.fat.extend(struct.unpack_from("<%dI" % (self.sector // 4), self.sector_bytes(sid)))
        directory = self.chain_bytes(first_dir)
        self.entries = []
        for off in range(0, len(directory) - 127, 128):
            e = directory[off:off + 128]
            name_len = struct.unpack_from("<H", e, 0x40)[0]
            kind = e[0x42]
            if kind not in (CFB_STORAGE, CFB_STREAM, CFB_ROOT) or not 2 <= name_len <= 64:
                continue
            name = e[:name_len - 2].decode("utf-16-le", "replace")
            start, size = struct.unpack_from("<II", e, 0x74)
            self.entries.append((name, kind, start, size))
        root = next((e for e in self.entries if e[1] == CFB_ROOT), None)
        self.ministream = self.chain_bytes(root[2]) if root else b""
        self.minifat = []
        mf = self.chain_bytes(first_mini) if first_mini < CFB_END else b""
        self.minifat = list(struct.unpack_from("<%dI" % (len(mf) // 4), mf))

    def sector_bytes(self, sid):
        off = (sid + 1) << self.shift
        return self.data[off:off + self.sector]

    def chain_bytes(self, sid, fat=None, read=None, limit=None):
        fat = self.fat if fat is None else fat
        read = self.sector_bytes if read is None else read
        out, seen = [], 0
        while sid < CFB_END and sid < len(fat) and seen <= len(fat):
            out.append(read(sid))
            seen += 1
            if limit is not None and sum(map(len, out)) >= limit:
                break
            sid = fat[sid]
        return b"".join(out)

    def names(self):
        return {e[0] for e in self.entries}

    def stream(self, name, limit=4096):
        """Return the first limit bytes of the stream named name, b"" when
        there is none."""
        for n, kind, start, size in self.entries:
            if n != name or kind != CFB_STREAM:
                continue
            if size < self.cutoff:
                mini = 1 << self.mini_shift

                def read(sid):
                    return self.ministream[sid * mini:(sid + 1) * mini]

                data = self.chain_bytes(start, self.minifat, read, limit)
            else:
                data = self.chain_bytes(start, limit=limit)
            return data[:min(size, limit)]
        return b""


def compound(path):
    """Return the compound file at path, None when it is not one or is past
    MAX_CFB_BYTES."""
    try:
        with open(path, "rb") as f:
            data = f.read(MAX_CFB_BYTES + 1)
        if len(data) > MAX_CFB_BYTES:
            return None
        return CompoundFile(data)
    except (OSError, ValueError, struct.error, IndexError):
        return None


# BINARY_DENIED are the storages of macros and embedded objects: Word's and
# Excel's VBA projects, Word's ObjectPool of embedded OLE objects, and the
# MBD storages Excel keeps one embedded object each in.
BINARY_DENIED = ("Macros", "_VBA_PROJECT_CUR", "ObjectPool")


def binary_clean(cf):
    names = cf.names()
    return not any(n in names for n in BINARY_DENIED) and not any(n.startswith("MBD") for n in names)


def doc_qualifies(path):
    """A Word 97 to 2003 document (MS-DOC): a WordDocument stream whose FIB
    names Word 97 or later (nFib 0x00C1 and up) and is not encrypted
    (fEncrypted, MS-DOC 2.5.2), without macros or embedded objects. Word 6
    and 95 files, another binary format, are left out."""
    cf = compound(path)
    if cf is None or not binary_clean(cf):
        return False
    fib = cf.stream("WordDocument", 64)
    if len(fib) < 12:
        return False
    ident, nfib, _, _, flags = struct.unpack_from("<HHHHH", fib, 0)
    return ident == 0xA5EC and nfib >= 0x00C1 and not flags & 0x0100


def xls_qualifies(path):
    """An Excel 97 to 2003 workbook (MS-XLS): a Workbook stream opening on a
    BIFF8 BOF record (version 0x0600) not followed by FilePass, so it is not
    encrypted (MS-XLS 2.4.117), without macros or embedded objects. BIFF5
    and older workbooks (a Book stream) are left out."""
    cf = compound(path)
    if cf is None or not binary_clean(cf):
        return False
    wb = cf.stream("Workbook", 256)
    if len(wb) < 8:
        return False
    rtype, size, version = struct.unpack_from("<HHH", wb, 0)
    if rtype != 0x0809 or version != 0x0600 or len(wb) < 4 + size + 4:
        return False
    nxt = struct.unpack_from("<H", wb, 4 + size)[0]
    return nxt != 0x002F


def ppt_qualifies(path):
    """A PowerPoint 97 to 2003 presentation (MS-PPT): a PowerPoint Document
    and a Current User stream, and no EncryptedSummary stream, which an
    encrypted presentation carries (MS-PPT 2.3.7). Embedded objects live
    inside the PowerPoint Document stream and are not looked for."""
    cf = compound(path)
    if cf is None:
        return False
    names = cf.names()
    return "PowerPoint Document" in names and "Current User" in names and "EncryptedSummary" not in names


# QUALIFIES maps a format to its qualifying test.
QUALIFIES = {
    "docx": docx_qualifies,
    "odt": odt_qualifies,
    "xlsx": xlsx_qualifies,
    "ods": ods_qualifies,
    "csv": csv_qualifies,
    "pptx": pptx_qualifies,
    "odp": odp_qualifies,
    "doc": doc_qualifies,
    "xls": xls_qualifies,
    "ppt": ppt_qualifies,
}


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


# UNSAFE_ID finds the characters a fixture ID does not carry: an ID stays
# plain ASCII, so every shell and form field passes it intact.
UNSAFE_ID = re.compile(r"[^A-Za-z0-9._-]+")


def plain_id(rel):
    """Return the fixture ID of the path rel, "/"-separated: each segment
    with its runs of other characters written "_"."""
    return "/".join(UNSAFE_ID.sub("_", seg).strip("_") or "_" for seg in rel.split("/"))


def select(checkout, subdir, fmt, exts, cap, max_bytes, skip=lambda rel: False):
    """Walk subdir of the checkout and return the (id, path, sha256) of the
    documents of fmt to keep: those of an extension in exts, at most
    max_bytes, not skipped, and qualifying, one each of identical bytes,
    sorted by ID, the first cap kept. A document's path is its path in the
    checkout, its ID and what skip reads its path under subdir. Also return
    the candidate and qualifying counts."""
    qualifies = QUALIFIES[fmt]
    root = os.path.join(checkout, subdir)
    seen, found, candidates = set(), [], 0
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in sorted(filenames):
            if not name.lower().endswith(exts):
                continue
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, root).replace(os.sep, "/")
            if skip(rel) or os.path.getsize(path) > max_bytes:
                continue
            candidates += 1
            if not qualifies(path):
                continue
            digest = sha256(path)
            if digest in seen:
                continue
            seen.add(digest)
            found.append((plain_id(rel), os.path.relpath(path, checkout).replace(os.sep, "/"), digest))
    found.sort()
    ids = [f[0] for f in found]
    if len(set(ids)) != len(ids):
        raise SystemExit("two documents of %s share an ID once made plain" % fmt)
    return found[:cap], candidates, len(found)


def write_lists(out, kept, licence):
    """Write documents.json ({"id", "path"} each, path the file's in its
    source) and files.sha256 ("<sha256>  <path>" for each document, then
    the licence files, (path, sha256) pairs) into the corpus directory out."""
    with open(os.path.join(out, "documents.json"), "w", encoding="utf-8") as f:
        json.dump([{"id": i, "path": p} for i, p, _ in kept], f, indent=2)
        f.write("\n")
    with open(os.path.join(out, "files.sha256"), "w", encoding="utf-8") as f:
        for _, p, h in kept:
            f.write("%s  %s\n" % (h, p))
        for p, h in licence:
            f.write("%s  %s\n" % (h, p))
