#!/usr/bin/env python3
"""Build ttnoenc_2b.pdf: embedded simple TrueType fonts that carry no /Encoding.

ISO 32000-1, 9.6.6.4 maps the codes of a non-symbolic TrueType font through
/Encoding, and a font dictionary that omits the key leaves no base encoding to
name the codes by. veraPDF 1.30.2 then reads such a font as it reads a symbolic
one (`TrueTypeFontProgram`: every branch tests `isSymbolic ||
encoding.getDirectBase() == null`): a code reaches a glyph only through a
Microsoft Symbol (3,0) or a Macintosh Roman (1,0) subtable, the glyph has no
name, and so no Unicode value comes from the encoding either. The (3,1)
subtable, which a non-symbolic font is otherwise looked up through, is skipped.

The fixture draws five fonts, the same two codes each, and differs only in the
'cmap' subtables the programs carry:

  - N31: non-symbolic, (3,1) only. No (3,0) and no (1,0), so the drawn glyphs
    are not present for glyph presence.
  - N3110: non-symbolic, (3,1) and (1,0). The (1,0) finds both glyphs.
  - N10: non-symbolic, (1,0) only.
  - N30: non-symbolic, (3,0) only, keyed in the 0xF000 range.
  - S31: symbolic, (3,1) only, the symbolic twin of N31.

Each font carries /Widths matching its program, so the width clause stays
silent wherever the glyph is found. No font carries /ToUnicode, so the Unicode
clauses of parts 2u, 2a, 3u, 3a and PDF/UA read whatever the encoding names.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

# Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
# one (ISO 32000-1, Table 123).
SYMBOLIC = 4
NONSYMBOLIC = 32

LATIN = {0x41: "A", 0x42: "B"}
SYMBOL_RANGE = {0xF041: "A", 0xF042: "B"}


def truetype(name, flags, subtables):
    """One simple TrueType font dictionary with an embedded program and no /Encoding."""
    program = _truetype.build(subtables)
    font_file = Stream(pdf, program)
    font_file.Length1 = len(program)
    descriptor = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=flags,
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontBBox=Array([0, -200, 550, 700]),
        FontFile2=pdf.make_indirect(font_file),
    )
    font = Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/" + name),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        FontDescriptor=pdf.make_indirect(descriptor),
    )
    return pdf.make_indirect(font)


fonts = Dictionary(
    N31=truetype("LithoNoEncUni", NONSYMBOLIC, [(3, 1, LATIN)]),
    N3110=truetype("LithoNoEncUniMac", NONSYMBOLIC, [(3, 1, LATIN), (1, 0, LATIN)]),
    N10=truetype("LithoNoEncMac", NONSYMBOLIC, [(1, 0, LATIN)]),
    N30=truetype("LithoNoEncSym30", NONSYMBOLIC, [(3, 0, {**LATIN, **SYMBOL_RANGE})]),
    S31=truetype("LithoNoEncSymUni", SYMBOLIC, [(3, 1, LATIN)]),
)

content = (
    b"BT /N31 24 Tf 50 700 Td (AB) Tj ET\n"
    b"BT /N3110 24 Tf 50 660 Td (AB) Tj ET\n"
    b"BT /N10 24 Tf 50 620 Td (AB) Tj ET\n"
    b"BT /N30 24 Tf 50 580 Td (AB) Tj ET\n"
    b"BT /S31 24 Tf 50 540 Td (AB) Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=fonts),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents, as every sibling fixture.
save(pdf, "ttnoenc_2b.pdf", min_version="1.7")
