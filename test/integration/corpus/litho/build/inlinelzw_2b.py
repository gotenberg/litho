#!/usr/bin/env python3
"""Build inlinelzw_2b.pdf: inline images whose filters the parts of ISO 19005
forbid, in every kind of stream veraPDF parses, for the repair that re-encodes
them inside their content stream.

ISO 19005-1, 6.1.10 forbids LZWDecode in an inline image's filters, and ISO
19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid LZWDecode, Crypt and every
value outside ISO 32000-1, Table 6. veraPDF judges each inline image as an
image of its own, so each forbidden image here fails the rule once:

  page   P1 writes /F /LZW with /DP << /EarlyChange 0 >> and /L, its data
         length. P2 writes /F [/AHx /LZW /RL]: the repair decodes through the
         LZW and keeps the RunLength after it. P3 writes /F /AHx, which every
         part permits, and is the control.
  Fm     a form XObject the page draws, whose image writes /Filter /LZWDecode,
         the full key and name.
  T3     the procedure of glyph a of a Type 3 font the page shows, whose image
         writes /F [/LZW].
  Pt     the cell of a tiling pattern the page fills with, whose image writes
         /F /LZW.
  Ap     the normal appearance of a Square annotation, whose image writes
         /F /Lzw: a name outside Table 6, which parts 2 to 4 forbid and part 1
         does not.

Every image draws the same eight grey samples, and its data is real LZW, so a
repair that moves a pixel shows.

See ../README.md for the authoritative expected firing set.
"""

import binascii

from _common import Array, Dictionary, Name, Stream, lzw_encode, pikepdf, save

pdf = pikepdf.new()

SAMPLES = bytes([0x00, 0x40, 0x80, 0xFF, 0xFF, 0x80, 0x40, 0x00])


def runlength(data):
    """RunLength-encode data as one literal run (ISO 32000-1, 7.4.5)."""
    return bytes([len(data) - 1]) + data + b"\x80"


def inline(entries, data):
    # A reader finds the end of inline image data by scanning for EI, so no
    # planted payload may hold one.
    assert b"EI" not in data, entries
    return b"q 10 0 0 10 0 0 cm BI /W 4 /H 2 /CS /G /BPC 8 " + entries + b" ID " + data + b" EI Q\n"


lzw = lzw_encode(SAMPLES)
lzw0 = lzw_encode(SAMPLES, early_change=0)
chained = binascii.hexlify(lzw_encode(runlength(SAMPLES))).upper() + b">"

fm = Stream(pdf, inline(b"/Filter /LZWDecode", lzw))
fm.Type = Name.XObject
fm.Subtype = Name.Form
fm.BBox = Array([0, 0, 100, 100])
fm.Resources = Dictionary()
fm = pdf.make_indirect(fm)

glyph_a = pdf.make_indirect(Stream(pdf, b"1000 0 d0 " + inline(b"/F [/LZW]", lzw)))
t3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=glyph_a),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a])),
        FirstChar=97,
        LastChar=97,
        Widths=Array([1000]),
        Resources=Dictionary(),
    )
)

pt = Stream(pdf, inline(b"/F /LZW", lzw))
pt.Type = Name.Pattern
pt.PatternType = 1
pt.PaintType = 1
pt.TilingType = 1
pt.BBox = Array([0, 0, 10, 10])
pt.XStep = 10
pt.YStep = 10
pt.Resources = Dictionary()
pt = pdf.make_indirect(pt)

ap = Stream(pdf, inline(b"/F /Lzw", lzw))
ap.Type = Name.XObject
ap.Subtype = Name.Form
ap.BBox = Array([0, 0, 40, 40])
ap.Resources = Dictionary()
ap = pdf.make_indirect(ap)
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([10, 10, 50, 50]),
        F=4,
        AP=Dictionary(N=ap),
    )
)

content = pdf.make_indirect(
    Stream(
        pdf,
        inline(b"/F /LZW /DP << /EarlyChange 0 >> /L " + str(len(lzw0)).encode(), lzw0)
        + inline(b"/F [/AHx /LZW /RL]", chained)
        + inline(b"/F /AHx", binascii.hexlify(SAMPLES).upper() + b">")
        + b"q 100 0 0 100 100 400 cm /Fm Do Q\n"
        b"BT /T3 12 Tf 10 700 Td (a) Tj ET\n"
        b"q /Pattern cs /Pt scn 0 0 50 50 re f Q\n",
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(T3=t3), XObject=Dictionary(Fm=fm), Pattern=Dictionary(Pt=pt)),
        Contents=content,
        Annots=Array([annot]),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "inlinelzw_2b.pdf", min_version="1.7")
