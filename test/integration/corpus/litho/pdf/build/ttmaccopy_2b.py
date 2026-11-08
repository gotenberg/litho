#!/usr/bin/env python3
"""Build ttmaccopy_2b.pdf: symbolic TrueType fonts whose (1,0) subtable copies (3,1).

ISO 32000-1, 9.6.6.4 has a symbolic TrueType font with no /Encoding and no
(3,0) subtable select its glyphs through the (1,0) Macintosh subtable, whose
codes are Mac OS Roman characters. Some producers write the (3,1) mapping under
the (1,0) record instead: the "Mac" table then carries no meaning of its own,
and reading its codes as Mac OS Roman names the wrong characters. litho reads
such a copy through the glyphs its codes select, and keeps the Mac OS Roman
reading of a genuine (1,0) subtable. pdfjs issue15910.pdf is the one corpus
file that reaches the construct.

| Font | (1,0) subtable             | (3,1) subtable             | Shows | Text |
| ---- | -------------------------- | -------------------------- | ----- | ---- |
| M1   | C4 Adieresis, E4 adieresis | C4 Adieresis, E4 adieresis | C4 E4 | Ää   |
| M2   | 80 Adieresis, 8A adieresis | C4 Adieresis, E4 adieresis | 80 8A | Ää   |

M1 is the copy: read as Mac OS Roman, C4 and E4 are "ƒ‰". M2 is the control, a
genuine Mac OS Roman table: read as anything else, 80 and 8A are "€Š".

Both fonts are symbolic, embedded and drawn, and declare two subtables with no
(3,0), so 6.2.11.6 t4 fires once per program; neither declares /Encoding, so
6.2.11.6 t3 stays silent. Every count was confirmed with live veraPDF 1.30.2.
See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

SYMBOLIC = 4
NAMES = (".notdef", "Adieresis", "adieresis")
UNICODE = {0xC4: "Adieresis", 0xE4: "adieresis"}
MAC_ROMAN = {0x80: "Adieresis", 0x8A: "adieresis"}


def truetype(name, subtables, first, last):
    """One symbolic TrueType font dictionary with no /Encoding."""
    program = _truetype.build(subtables, glyph_names=NAMES)
    font_file = Stream(pdf, program)
    font_file.Length1 = len(program)
    descriptor = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=SYMBOLIC,
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontBBox=Array([0, -200, 550, 700]),
        FontFile2=pdf.make_indirect(font_file),
    )
    return pdf.make_indirect(Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/" + name),
        FirstChar=first,
        LastChar=last,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (last - first + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
    ))


m1 = truetype("LithoMacCopy", [(1, 0, UNICODE), (3, 1, UNICODE)], 0xC4, 0xE4)
m2 = truetype("LithoMacGenuine", [(1, 0, MAC_ROMAN), (3, 1, UNICODE)], 0x80, 0x8A)

content = (
    b"BT /M1 24 Tf 50 700 Td <C4E4> Tj ET\n"
    b"BT /M2 24 Tf 50 660 Td <808A> Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(M1=m1, M2=m2)),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "ttmaccopy_2b.pdf", min_version="1.7")
