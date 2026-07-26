#!/usr/bin/env python3
"""Build trailer_2b.pdf: trailer and file-structure defects.

The coverage scan against the veraPDF profile showed the whole ISO 19005-2,
6.1 family unvisited by this corpus. Most of it needs byte-level malformation
that pikepdf will not emit, but the trailer rules of 6.1.3 are reachable: two
by writing the file normally and then editing the bytes, one by encrypting.

All three defects are independent and live in different parts of the file, so
a check that stops after the first trailer problem reports one rule where
three are due. Encryption is the interesting one: it changes how every string
and stream in the file is read, so a reader that gives up on it reports
nothing at all rather than reporting the encryption itself.

This is the one fixture in the corpus that does NOT rebuild byte for byte.
qpdf derives a random file encryption key on every save and exposes no way to
pin it, so two runs of this script differ inside /O, /U and every encrypted
string. Regenerate it deliberately; do not diff it against a fresh build the
way the other builders allow.

See ../README.md for the authoritative expected firing set.
"""

import os

import pikepdf

from _common import Array, Dictionary, Name, Stream, String, out_path, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (encrypted body) Tj ET\n"),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents, so the file still carries
# the document-level defects the rest of the corpus relies on.
save(pdf, "trailer_2b.pdf", min_version="1.7")

# Re-save encrypted. ISO 19005-2, 6.1.3 t2 forbids /Encrypt outright, and the
# owner password keeps the body readable to a tool that implements RC4/AES so
# the remaining rules stay reachable rather than being masked by a file that
# cannot be parsed at all.
path = out_path("trailer_2b.pdf")
with pikepdf.open(path, allow_overwriting_input=True) as doc:
    doc.save(
        path,
        encryption=pikepdf.Encryption(owner="owner", user="", aes=True, R=4),
        static_id=True,
    )

# Append junk after the final %%EOF. ISO 19005-2, 6.1.3 t3 allows only a
# single optional end-of-line marker there.
with open(path, "ab") as fh:
    fh.write(b"trailing junk after the last EOF marker\n")

size = os.path.getsize(path)
print("wrote", path, size, "bytes")
