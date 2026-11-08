#!/usr/bin/env python3
"""Build formhidden_ua1.pdf: widgets a viewer never displays, under the form mutations.

ISO 32000-1, Table 165: a Hidden annotation is neither displayed nor printed,
and a NoView one is not displayed and prints only when its Print flag is set.
Flattening the form drops such a widget with its field rather than bake its
appearance into the page. Content that shows only in print needs an optional
content group whose /AS applies a print usage (ISO 32000-1, 8.11.4.4), which
ISO 14289-1, 7.10 and ISO 14289-2, 8.7 forbid, so a printed NoView widget goes
as well. A Form element left holding nothing leaves the tree with its widget.

Six text fields on one tagged page, each with an appearance painting a box and
a /TU (ISO 14289-1, 7.18.1 test 3):

  * "secret", /F 6 (Hidden, Print), the only kid of its Form element.
  * "draft", /F 2 (Hidden), outside the structure tree: ISO 14289-1, 7.18.4
    test 1 exempts a hidden widget from the Form tag.
  * "stamp", /F 36 (NoView, Print), in a Form element: printed only.
  * "note", /F 32 (NoView), in a Form element: neither displayed nor printed.
  * "tip", /F 292 (NoView, ToggleNoView, Print), in a Form element, which
    ISO 14289-2, 8.9.2.2 test 2 permits where it fails "stamp" and "note".
  * "name", /F 4, displayed: flattening bakes it and tags the content under its
    Form element, which takes the PrintField role tv.

The page's text shows only A and B in the conforming embedded TrueType of
ttcmap_2b.pdf, so no font clause fires. The XMP packet carries dc:title and
pdfuaid:part 1.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoHiddenSans"),
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
        BaseFont=Name("/LithoHiddenSans"),
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


def field(name, flags, y, grey, struct_parent=None):
    """A text field merged with its widget, painting a grey box."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, y, 70, y + 20]),
        F=flags,
        P=page,
        FT=Name.Tx,
        T=String(name),
        TU=String(name.capitalize()),
        V=String(name),
        AP=Dictionary(N=appearance(b"%s g 0 0 20 20 re f\n" % grey)),
    )
    if struct_parent is not None:
        d.StructParent = struct_parent
    return pdf.make_indirect(d)


# ISO 32000-1, Table 165: bit 2 Hidden, bit 3 Print, bit 6 NoView, bit 9
# ToggleNoView.
secret = field("secret", 2 | 4, 700, b"0.1", 1)
draft = field("draft", 2, 660, b"0.2")
stamp = field("stamp", 32 | 4, 620, b"0.3", 2)
note = field("note", 32, 580, b"0.4", 3)
tip = field("tip", 32 | 4 | 256, 540, b"0.5", 4)
name = field("name", 4, 500, b"0.6", 5)
widgets = [secret, draft, stamp, note, tip, name]
page.Annots = Array(widgets)
pdf.Root.AcroForm = Dictionary(Fields=Array(widgets))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)
paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0)
)


def form(widget):
    """A Form element whose only kid is an object reference to widget."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Form,
            P=document,
            Pg=page,
            K=Dictionary(Type=Name.OBJR, Obj=widget),
        )
    )


form_secret = form(secret)
form_stamp = form(stamp)
form_note = form(note)
form_tip = form(tip)
form_name = form(name)

document.K = Array([paragraph, form_secret, form_stamp, form_note, form_tip, form_name])
struct_root.K = document
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([paragraph]),
                1,
                form_secret,
                2,
                form_stamp,
                3,
                form_note,
                4,
                form_tip,
                5,
                form_name,
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 6

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "formhidden_ua1.pdf", min_version="1.7")
