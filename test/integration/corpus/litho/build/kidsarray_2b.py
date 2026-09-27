#!/usr/bin/env python3
"""Build kidsarray_2b.pdf: pages written directly in a /Kids array that its
node names by reference, at PDF/A-2b.

ISO 32000-1, 7.7.3.2, Table 29 makes /Kids an array of indirect references. A
Writer gives every page written directly in one an object number before it
edits or repairs the document, and has the array name it by reference.
kidsrepair_1b.pdf holds two pages written directly in a /Kids array written
inline in its node; no corpus file holds one in an array its node names by
reference, where the array, not the node, is what the numbering rewrites:

| Page | Where it sits                                    | Shows | Carries         |
| ---- | ------------------------------------------------ | ----- | --------------- |
| 1    | the root's /Kids, by reference                   | A     | nothing         |
| 2    | the array node L names as its /Kids by reference | B     | a field, an /AA |
| 3    | the same array, after page 2                     | AB    | a note, no /F   |

Each page states its own /Resources and shows its text in the conforming
embedded TrueType of ttcmap_2b.pdf, so no font or resource clause fires; the
text paints in the initial DeviceGray, with no output intent. Page 2 holds a
text field's widget, which the form mutations flatten, and a page /AA holding a
permitted GoTo action (ISO 19005-2, 6.5.2 test 2). Page 3 holds a Text
annotation with no /F (6.3.2 test 1). Every appearance paints nothing.

veraPDF 1.30.2 refuses a file whose page tree holds an intermediate node
written directly in /Kids ("doesn't appear to be a valid PDF"), so the other
shape the numbering handles has no fixture.

The tree is built by hand: pikepdf's page list flattens it, which would delete
the construct the fixture exists for.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoKidsSans"),
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
        BaseFont=Name("/LithoKidsSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)


def resources():
    return Dictionary(Font=Dictionary(F1=font))


def content(text):
    return pdf.make_indirect(Stream(pdf, b"BT /F1 24 Tf 72 700 Td (" + text + b") Tj ET\n"))


def appearance():
    """A 20 by 20 Form XObject appearance that paints nothing."""
    s = Stream(pdf, b"0 0 20 20 re n\n")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(s)


root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 612, 792])

page1 = pdf.make_indirect(Dictionary(Type=Name.Page, Parent=root, Resources=resources(), Contents=content(b"A")))
node_l = pdf.make_indirect(Dictionary(Type=Name.Pages, Parent=root, Count=2))

field = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        FT=Name.Tx,
        T=String("city"),
        V=String(""),
        Rect=Array([72, 600, 92, 620]),
        F=4,
        AP=Dictionary(N=appearance()),
    )
)
page2 = Dictionary(
    Type=Name.Page,
    Parent=node_l,
    Resources=resources(),
    Contents=content(b"B"),
    Annots=Array([field]),
    AA=Dictionary(O=Dictionary(S=Name.GoTo, D=Array([page1, Name.Fit]))),
)

note = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Text,
        Rect=Array([72, 500, 92, 520]),
        Contents=String("note"),
        AP=Dictionary(N=appearance()),
    )
)
page3 = Dictionary(
    Type=Name.Page,
    Parent=node_l,
    Resources=resources(),
    Contents=content(b"AB"),
    Annots=Array([note]),
)
node_l.Kids = pdf.make_indirect(Array([page2, page3]))

root.Kids = Array([page1, node_l])
root.Count = 3

pdf.Root.AcroForm = Dictionary(Fields=Array([field]))
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "kidsarray_2b.pdf")
