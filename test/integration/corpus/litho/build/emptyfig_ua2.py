#!/usr/bin/env python3
"""Build emptyfig_ua2.pdf: a tagged document whose Figures lack alternate text,
most of them enclosing nothing, each named by a different kind of reference.

ISO 14289-2, 8.2.5.28.2 and ISO 14289-1, 7.3 ask every Figure for alternate
text. Aggressive PDF/UA remediation removes a Figure that lacks it and encloses
nothing, since no text could describe it, and cleans every reference to it. No
corpus file reaches most of the forms an empty Figure takes, or most of the
references the removal cleans, so this file packs them into two pages:

  * F1's only kid is the null object.
  * F2 lists MCID 7, which page 1 does not mark; the page's /ParentTree slot 7
    names it.
  * F3 holds an object reference whose /Obj names object 999, which the file
    does not define and ISO 32000-1, 7.3.10 reads as null, and a /ParentTree
    entry of its own, under key 5, which no annotation names. qpdf writes no
    reference to an undefined object, so the builder patches it in, keeping
    every offset.
  * F4 holds F5 alone, and F5 a marked-content reference to MCID 8, which page 1
    does not mark: both go, the cascade running through Figures.
  * F6 carries /Alt and holds F7, which has no kid: F7 goes and F6 stays, empty.
  * A Div holds F8 alone, whose MCID 9 page 1 does not mark: F8 goes and the Div
    stays, empty.
  * F9's MCID 10 is not marked either. The /IDTree names F9, the paragraph's /Ref
    names it, and the outline item `Figure` names it in /SE and in the /SD of its
    GoTo action to page 1, which then names the element page 1 opens on.
  * F10's MCID 0 on page 2, which marks nothing, is the only element on page 2,
    and the outline item `Page two` names it in the /SD of its GoTo action to
    page 2. Removing F10 would leave that /SD no element to name and the
    action its page destination alone, which ISO 14289-2, 8.8 rejects, so F10
    stays.

Three Figures lacking alternate text stay: F10, K2, whose MCID 2 the page
marks around a drawing, and K3, which has no kid but which the outline item
`Pinned` names in a destination array, where no page destination stands
beside it.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    out_path,
    pikepdf,
    save,
    xmp_packet,
)

# Stands in for "999 0 R " until the patch below: the same eight bytes.
UNDEFINED_PLACEHOLDER = b"/ObjNone"
UNDEFINED_REF = b"999 0 R "

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoEmptyFig"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoEmptyFig"),
                Flags=32,
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile2=pdf.make_indirect(font_file),
            )
        ),
    )
)

page1_content = (
    b"/H1 <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AAAB) Tj ET\nEMC\n"
    b"/P <</MCID 1>> BDC\nBT /F1 12 Tf 50 700 Td (AABA) Tj ET\nEMC\n"
    b"/Figure <</MCID 2>> BDC\n50 600 m 150 650 l S\nEMC\n"
)
page2_content = b"/Artifact BMC\n50 50 m 100 50 l S\nEMC\n"

pages = []
for i, (content, resources) in enumerate(
    [
        (page1_content, Dictionary(Font=Dictionary(F1=font))),
        (page2_content, Dictionary()),
    ]
):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=resources,
            Contents=Stream(pdf, content),
            StructParents=i,
            Tabs=Name.S,
        )
    )
    pdf.pages.append(pikepdf.Page(page))
    pages.append(pdf.pages[i].obj)
page1, page2 = pages

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, parent, page=None, k=None, **extra):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns20, **extra)
    if page is not None:
        d.Pg = page
    if k is not None:
        d.K = k
    return pdf.make_indirect(d)


title = elem("H1", document, page1, 0)
para = elem("P", document, page1, 1)
k2 = elem("Figure", document, page1, 2)
k3 = elem("Figure", document, page1)

f1 = elem("Figure", document, page1, Array([None]))
f2 = elem("Figure", document, page1, 7)
f3 = elem(
    "Figure",
    document,
    page1,
    Array([Dictionary(Type=Name.OBJR, Obj=Name(UNDEFINED_PLACEHOLDER.decode()))]),
)
f4 = elem("Figure", document, page1)
f5 = elem(
    "Figure",
    f4,
    None,
    Array([Dictionary(Type=Name.MCR, Pg=page1, MCID=8)]),
)
f4.K = Array([f5])
f6 = elem("Figure", document, page1, Alt=String("A chart"))
f7 = elem("Figure", f6, page1)
f6.K = Array([f7])
div = elem("Div", document)
f8 = elem("Figure", div, page1, 9)
div.K = Array([f8])
f9 = elem("Figure", document, page1, 10, ID=String("fig9"))
para.Ref = Array([f9])
f10 = elem("Figure", document, page2, 0)

document.K = Array([title, para, k2, k3, f1, f2, f3, f4, f6, div, f9, f10])
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.IDTree = Dictionary(Names=Array([String("fig9"), f9]))
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([title, para, k2, None, None, None, None, f2, f5, f8, f9]),
                1,
                Array([f10]),
                5,
                f3,
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 6

outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines))
item_figure = pdf.make_indirect(
    Dictionary(
        Title=String("Figure"),
        Parent=outlines,
        SE=f9,
        A=Dictionary(
            S=Name.GoTo, D=Array([page1, Name.Fit]), SD=Array([f9, Name.Fit])
        ),
    )
)
item_page2 = pdf.make_indirect(
    Dictionary(
        Title=String("Page two"),
        Parent=outlines,
        Prev=item_figure,
        A=Dictionary(
            S=Name.GoTo, D=Array([page2, Name.Fit]), SD=Array([f10, Name.Fit])
        ),
    )
)
item_pinned = pdf.make_indirect(
    Dictionary(
        Title=String("Pinned"),
        Parent=outlines,
        Prev=item_page2,
        Dest=Array([k3, Name.Fit]),
    )
)
item_figure.Next = item_page2
item_page2.Next = item_pinned
outlines.First = item_figure
outlines.Last = item_pinned
outlines.Count = 3

pdf.Root.Outlines = outlines
pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "emptyfig_ua2.pdf", min_version="2.0")

path = out_path("emptyfig_ua2.pdf")
with open(path, "rb") as fh:
    data = fh.read()
if len(UNDEFINED_PLACEHOLDER) != len(UNDEFINED_REF):
    raise SystemExit("F3: the patch changes length, offsets would move")
if data.count(UNDEFINED_PLACEHOLDER) != 1:
    raise SystemExit(f"F3: expected one placeholder, found {data.count(UNDEFINED_PLACEHOLDER)}")
with open(path, "wb") as fh:
    fh.write(data.replace(UNDEFINED_PLACEHOLDER, UNDEFINED_REF))
