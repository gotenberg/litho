#!/usr/bin/env python3
"""Build ttcmap_2b.pdf: 'cmap' subtable requirements on embedded TrueType programs.

The coverage scan showed ISO 19005-2, 6.2.11.6 tests 1 and 4 unreached, and
`ttencoding_2b.pdf` explains why: those two are scoped to `TrueTypeFontProgram`
rather than to the font dictionary, so they need a real embedded program whose
'cmap' table carries particular (platformID, encodingID) subtables. No corpus
fixture embedded a TrueType program at all. `_truetype.py` emits them, so the
builders still depend on nothing but pikepdf.

The two tests are mutually exclusive in one font, since t1 judges non-symbolic
programs and t4 symbolic ones, so the fixture carries four sibling fonts: one
defective of each kind and one conforming control of each kind. All four are
drawn, because an undrawn font reaches neither test. Planting both defects at
once is the masking probe: both are `TrueTypeFontProgram` rules, so a check
that reports the first offending program and stops reports one where two are
due, and the two controls catch a check that condemns every embedded program
once it sees one.

The part split is real and is exercised by the same file at its other profiles.
Parts 2 and 3 and PDF/UA-1 ask for at least one subtable, and more than one when
a Microsoft Symbol (3,0) is present. PDF/A-4 and PDF/UA-2 instead ask for a
Microsoft Unicode (3,1) or a Macintosh Roman (1,0), whatever the count. PDF/A-1
states no non-symbolic requirement at all. The non-symbolic defect here carries
only a (3,0), which every part that states the rule rejects, so the cell moves
with the profile rather than staying silent outside part 2.

The glyph names are carried in a 'post' table in format 2.0 on purpose. Without
them a non-symbolic font whose 'cmap' offers no usable subtable fails the glyph
lookup too and drags in 6.2.11.4.1 t2, which would mask the 'cmap' verdict this
fixture is about. Measured on both tools: format 3.0 adds that clause, format
2.0 does not.

Per part the clause is ISO 19005-2 and -3, 6.2.11.6, ISO 19005-4, 6.2.10.6,
ISO 14289-1, 7.21.6, and ISO 14289-2, 8.4.5.7.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

# Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
# one. Per ISO 32000-1, Table 123 the two are exclusive, and both tests this
# fixture reaches turn on which one is set.
SYMBOLIC = 4
NONSYMBOLIC = 32

# The codes the page draws. A symbolic (3,0) subtable is conventionally keyed in
# the 0xF000 range, so the symbolic fonts map both forms and a viewer resolves
# the glyph either way.
LATIN = {0x41: "A", 0x42: "B"}
SYMBOL_RANGE = {0xF041: "A", 0xF042: "B"}


def truetype(name, flags, subtables, encoding):
    """One simple TrueType font dictionary with an embedded program."""
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
    if encoding is not None:
        font.Encoding = Name("/" + encoding)
    return pdf.make_indirect(font)


# T1: non-symbolic, and its program carries only a Microsoft Symbol (3,0), which
# supplies no non-symbolic lookup. Fires t1.
t1 = truetype(
    "LithoNonSymOnly30",
    NONSYMBOLIC,
    [(3, 0, {**LATIN, **SYMBOL_RANGE})],
    "WinAnsiEncoding",
)
# T2: symbolic, and its program carries two subtables, neither of them (3,0).
# Fires t4. It declares no /Encoding, so 6.2.11.6 t3 stays silent.
t2 = truetype("LithoSymTwoNo30", SYMBOLIC, [(1, 0, LATIN), (3, 1, LATIN)], None)
# T3 and T4 conform, so a firing against either is over-suppression.
t3 = truetype("LithoNonSymOK", NONSYMBOLIC, [(3, 1, LATIN), (1, 0, LATIN)], "WinAnsiEncoding")
t4 = truetype("LithoSymOK30", SYMBOLIC, [(3, 0, {**LATIN, **SYMBOL_RANGE})], None)

# An ExtGState carrying a transfer function, which ISO 19005-2, 6.2.5 forbids
# outright. It belongs to no font clause, so it shows a 'cmap' verdict does not
# leak into unrelated checks, and gives the page a second independent defect.
transfer = Stream(pdf, b"{ 1 exch sub }")
transfer.FunctionType = 4
transfer.Domain = Array([0, 1])
transfer.Range = Array([0, 1])
gs = pdf.make_indirect(Dictionary(Type=Name.ExtGState, TR=pdf.make_indirect(transfer)))

# The two defective fonts are drawn inside a q/Q pair whose Q restores the font
# selected before it. Per ISO 32000-1, 8.4.2 and Table 104 the text font is part
# of the graphics state, so a validator that ignores the restore attributes the
# last two codes to the wrong font and reports the wrong program.
content = (
    b"q /GS1 gs 0 0 0 rg 50 730 40 20 re f Q\n"
    b"BT /T3 24 Tf 50 700 Td (AB) Tj ET\n"
    b"q BT /T1 24 Tf 50 660 Td (AB) Tj ET\n"
    b"BT /T2 24 Tf 50 620 Td (AB) Tj ET Q\n"
    b"BT 50 580 Td (AB) Tj ET\n"
    b"BT /T4 24 Tf 50 540 Td (AB) Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(T1=t1, T2=t2, T3=t3, T4=t4),
        ExtGState=Dictionary(GS1=gs),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "ttcmap_2b.pdf", min_version="1.7")
