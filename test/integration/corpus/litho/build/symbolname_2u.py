#!/usr/bin/env python3
"""Build symbolname_2u.pdf: a font's name plays no part in the Unicode value
of its glyphs.

ISO 19005-2, 6.2.11.7.2 asks a Unicode value of every shown glyph, which ISO
32000-1, 9.10.2 derives from the glyph name the encoding gives the code. A
/BaseFont that contains "Symbol" says nothing about the glyph a code selects.

  MS  MdSymbolA-Regular, a nonsymbolic TrueType embedding the three-glyph
      program with its glyphs named A and H20850, under a /Differences array
      naming codes 41 A and 42 H20850 and no /BaseEncoding. Its descriptor
      carries a /CharSet of /alpha/beta, two Symbol-font names, which ISO
      32000-1, Table 122 makes meaningful for Type 1 fonts alone. It shows 41
      and 42: 41 has a Unicode value, H20850 has none in the Adobe Glyph List,
      the Symbol font or ZapfDingbats, so 42 fails t1 once.

litho used to exempt the font: its name holds "Symbol" and its /CharSet lists
only Symbol-font names. veraPDF reads neither.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

NONSYMBOLIC = 32
GLYPHS = (".notdef", "A", "H20850")
PROGRAM = _truetype.build([(3, 1, {0x41: "A", 0x42: "H20850"})], glyph_names=GLYPHS)

program = Stream(pdf, PROGRAM)
program[Name.Length1] = len(PROGRAM)
descriptor = pdf.make_indirect(
    Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/MdSymbolA-Regular"),
        Flags=NONSYMBOLIC,
        FontBBox=Array([0, -200, 550, 700]),
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontFile2=pdf.make_indirect(program),
        CharSet=String("/alpha/beta"),
    )
)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/MdSymbolA-Regular"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([600, 600]),
        FontDescriptor=descriptor,
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A, Name.H20850])),
    )
)

body = b"BT 10 700 Td /MS 12 Tf <4142> Tj ET\n"
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(MS=font)),
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "symbolname_2u.pdf", min_version="1.7")
