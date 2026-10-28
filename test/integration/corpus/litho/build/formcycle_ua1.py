#!/usr/bin/env python3
"""Build formcycle_ua1.pdf: forms whose resources list each other, auto-tagged.

Remediating an untagged file at PDF/UA-1 or at a level-A PDF/A profile tags it,
and the tagging marks every region of page content that is not a structure
leaf as an /Artifact, except the regions that draw a Form XObject painting
text (ISO 14289-1, 7.1). It decides which forms paint text by following the
XObjects each form's resources list, and a form's resources can list a form
that lists them back. No form here invokes itself, which ISO 32000-1, 8.10.1
forbids: the cycle is in the resources, not in the drawing.

The page draws three forms between three lines of its own text:

  * /X2 is form B, which lists and draws A. A lists B and C and draws only C,
    and C shows text. The page also lists A as /X1 without drawing it. The
    scan met B from A, with A still on its path, cached "no text" for B, and
    swept /X2 into an /Artifact whenever Go's map order put /X1 first and B
    before C, about three runs in four. The name C shows then stopped
    extracting, and the integrity probe refused the remediation.
  * /X3 is form D, which draws E, which shows text: a chain with no cycle, the
    shape pdfjs issue15629 draws its field values with.
  * /X4 is form F, which paints a rectangle and no text: the control, which
    becomes an /Artifact.

The font is the conforming non-symbolic TrueType of ttcmap_2b.pdf, embedded,
and every string shows only A and B, the glyphs its program holds, so no font
clause fires. The file has no structure tree, no XMP packet and no /Lang.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoCycleSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoCycleSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)


def form(content, resources=None):
    """An indirect Form XObject drawing content, with resources if given."""
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 200, 20])
    if resources is not None:
        stream.Resources = resources
    return pdf.make_indirect(stream)


fonts = Dictionary(F1=font)

# C shows text; A draws C and lists B; B draws A. A and B are made first with
# empty resources and wired afterwards, since each names the other.
form_c = form(b"BT /F1 9 Tf 0 5 Td (AB) Tj ET", Dictionary(Font=fonts))
form_a = form(b"/C Do")
form_b = form(b"/A Do")
form_a.Resources = Dictionary(XObject=Dictionary(B=form_b, C=form_c))
form_b.Resources = Dictionary(XObject=Dictionary(A=form_a))

form_e = form(b"BT /F1 9 Tf 0 5 Td (BB) Tj ET", Dictionary(Font=fonts))
form_d = form(b"/E Do", Dictionary(XObject=Dictionary(E=form_e)))

form_f = form(b"0 0 200 20 re f")

content = (
    b"BT /F1 12 Tf 50 740 Td (AB) Tj ET\n"
    b"q 1 0 0 1 50 710 cm /X2 Do Q\n"
    b"BT /F1 12 Tf 50 680 Td (BA) Tj ET\n"
    b"q 1 0 0 1 50 650 cm /X3 Do Q\n"
    b"BT /F1 12 Tf 50 620 Td (ABBA) Tj ET\n"
    b"q 1 0 0 1 50 590 cm /X4 Do Q\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=fonts,
        XObject=Dictionary(X1=form_a, X2=form_b, X3=form_d, X4=form_f),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately untagged: no /StructTreeRoot, /MarkInfo, /Lang or /Metadata.
save(pdf, "formcycle_ua1.pdf", min_version="1.7")
