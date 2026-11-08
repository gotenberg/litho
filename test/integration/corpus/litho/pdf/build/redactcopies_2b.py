#!/usr/bin/env python3
"""Build redactcopies_2b.pdf: copies of page text no decoded value spells.

Redaction verifies its output by looking for every removed string in what a
rewrite writes. Two copies here sit where no decoded value spells them, and a
rewrite copies both:

* The catalog's /PieceInfo holds a FlateDecode stream whose stored data is
  a zlib stream of other words followed by the paragraph. Decoding stops at
  the zlib checksum (RFC 1950, 2.2), so the decoded data never shows the
  words, but the stored bytes carry them.
* The catalog's /Dests names a destination after the paragraph. Its keys are
  names (ISO 32000-1, 12.3.2.3), which a comparison of values never reads.

Before, redacting page 1 wrote both copies out and reported success. Now the
redaction refuses and names both survivors.

Page 2 draws text no redaction of page 1 touches.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import zlib

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)
resources = Dictionary(Font=Dictionary(F1=font))

WORDS = b"the secret plan"

page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(pdf, b"BT /F1 12 Tf 72 700 Td (" + WORDS + b") Tj ET\n"),
    )
)
page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(pdf, b"BT /F1 12 Tf 72 700 Td (page two) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page1))
pdf.pages.append(pikepdf.Page(page2))

# The stored data is written as it stands: _common.save keeps every declared
# filter and its bytes, so the words after the checksum stay in the file.
private = Stream(pdf, zlib.compress(b"unrelated notes") + WORDS)
private.Filter = Name.FlateDecode
pdf.Root.PieceInfo = Dictionary(
    LithoFixture=Dictionary(LastModified=pikepdf.String("D:20260101000000Z"), Private=private)
)

pdf.Root.Dests = pdf.make_indirect(
    Dictionary({"/" + WORDS.decode(): Array([page1, Name.Fit])})
)

save(pdf, "redactcopies_2b.pdf", min_version="1.7")
