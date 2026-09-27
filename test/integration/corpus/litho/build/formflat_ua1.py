#!/usr/bin/env python3
"""Build formflat_ua1.pdf: tagged widgets with no /AcroForm, for the form mutations.

Flattening a tagged form turns each widget's appearance into page content, and
ISO 14289-1, 7.1 requires that content to be tagged or an artifact. litho hands
the content to the structure element that enclosed the widget, and a Form
element that now holds content instead of its widget takes a PrintField /Role
(ISO 32000-1, Table 348), which ISO 14289-1, 7.18.4 test 2 asks of a Form
element that holds anything but one widget reference. A widget with nothing to
draw goes with the form, and an element left holding nothing goes with it.

Three widgets on one tagged page, each the only kid of its own Form element:

  * "empty", a text field with no value and no appearance: it draws nothing, so
    flattening drops it and its Form element with it.
  * the kid of "choice", a radio button that is on: its Form element states no
    /Pg, so the flattened content is named by a marked-content reference that
    carries the page, and the element takes the role rb.
  * "go", a push button: its Form element takes the role pb.

The catalog has no /AcroForm, a shape readers accept and real files have (pdfjs
issue17794.pdf), so moving a page gives the document a form, which has to list
the fields its own page already presents as well as the copies.

Every appearance paints a rectangle and no text, and the page's text shows only
A and B in the conforming embedded TrueType of ttcmap_2b.pdf, so no font clause
fires. The file has no XMP packet.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoFlatSans"),
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
        BaseFont=Name("/LithoFlatSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)


def appearance(content):
    """A 20 by 20 Form XObject appearance painting content."""
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(s)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AB) Tj ET\nEMC\n"),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))


def rect(y):
    return Array([50, y, 70, y + 20])


# A text field with no value and no appearance: nothing to draw.
empty = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=rect(700),
        F=4,
        P=page,
        FT=Name.Tx,
        T=String("empty"),
        TU=String("Empty"),
        StructParent=1,
    )
)

# A radio button group whose one kid is on (ISO 32000-1, Table 226: bit 16 is
# Radio, bit 15 NoToggleToOff).
choice = pdf.make_indirect(
    Dictionary(
        FT=Name.Btn,
        Ff=(1 << 15) | (1 << 14),
        T=String("choice"),
        TU=String("Choice"),
        V=Name("/On"),
    )
)
radio = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=rect(660),
        F=4,
        P=page,
        Parent=choice,
        AS=Name("/On"),
        AP=Dictionary(
            N=Dictionary(
                On=appearance(b"0 0 0 rg 5 5 10 10 re f\n"),
                Off=appearance(b"0 0 0 RG 1 1 18 18 re S\n"),
            )
        ),
        StructParent=2,
    )
)
choice.Kids = Array([radio])

# A push button (Table 226: bit 17 is Pushbutton).
push = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=rect(620),
        F=4,
        P=page,
        FT=Name.Btn,
        Ff=1 << 16,
        T=String("go"),
        TU=String("Go"),
        AP=Dictionary(N=appearance(b"0.5 g 0 0 20 20 re f\n")),
        StructParent=3,
    )
)
page.Annots = Array([empty, radio, push])

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)
paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0)
)


def form(widget, **extra):
    """A Form element whose only kid is an object reference to widget."""
    d = Dictionary(
        Type=Name.StructElem,
        S=Name.Form,
        P=document,
        K=Dictionary(Type=Name.OBJR, Obj=widget),
    )
    for key, value in extra.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


form_empty = form(empty, Pg=page)
form_radio = form(radio)  # no /Pg: the flattened content names its page
form_push = form(push, Pg=page)

document.K = Array([paragraph, form_empty, form_radio, form_push])
struct_root.K = document
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [0, Array([paragraph]), 1, form_empty, 2, form_radio, 3, form_push]
        )
    )
)
struct_root.ParentTreeNextKey = 4

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /AcroForm and no /Metadata.
save(pdf, "formflat_ua1.pdf", min_version="1.7")
