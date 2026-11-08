#!/usr/bin/env python3
"""Build outlinedest_ua2.pdf: a tagged document whose outline opens structure
destinations in every form a reader resolves, over pages drawing optional
content.

Split and merge rebuild the outline of the pages they copy, and a page
insertion copies a page's optional content into another document. No corpus
file packs the forms of a structure destination an outline item can name
(ISO 32000-2, 12.3.2.3) into one conforming file, and no corpus file names one
through a named destination's dictionary, so this file does:

  * a GoTo action naming page 1 under /D and its heading under /SD, both with
    the same /XYZ view (ISO 32000-2, 12.6.4.2, Table 202);
  * a child item whose /Dest is a structure destination array naming the
    first paragraph of page 2, and no page;
  * an item whose /Dest is a string naming a /Dests name tree entry whose
    value is a dictionary carrying /D, page 3, and /SD, its heading
    (ISO 32000-2, 12.3.2);
  * an item whose GoTo action's /D is that same string.

Page 1 draws a paragraph in a layer the default configuration shows and one in
a layer it hides, and /OCProperties carries a second configuration in /Configs,
each named and each ordering both groups (ISO 32000-1, 8.11.4.3; ISO 14289-2,
8.7; ISO 19005-2, 6.9), so `mutate-pages`, which copies page 1 to the end,
joins the copied groups to both configurations.

The font is embedded and draws only `A` and `B`, which is all the text uses.

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

PDF20_NS = "http://iso.org/pdf2/ssn"
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoOutlineDest"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoOutlineDest"),
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


def marked(tag, mcid, y, s, layer=None):
    text = b"/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n" % (
        tag,
        mcid,
        y,
        s,
    )
    if layer is None:
        return text
    return b"/OC /%s BDC\n%sEMC\n" % (layer, text)


shown = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Shown layer")))
hidden = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Hidden layer")))

contents = [
    marked(b"H1", 0, 740, b"AAAB")
    + marked(b"P", 1, 700, b"AABA", b"Shown")
    + marked(b"P", 2, 660, b"ABAA", b"Hidden"),
    marked(b"P", 0, 740, b"BAAA") + marked(b"P", 1, 700, b"BBAA"),
    marked(b"H1", 0, 740, b"BABA") + marked(b"P", 1, 700, b"BBBA"),
]
pages = []
for i, content in enumerate(contents):
    resources = Dictionary(Font=Dictionary(F1=font))
    if i == 0:
        resources.Properties = Dictionary(Shown=shown, Hidden=hidden)
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

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, parent, page=None, k=None):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns20)
    if page is not None:
        d.Pg = page
    if k is not None:
        d.K = k
    return pdf.make_indirect(d)


sect1 = elem("Sect", document)
title1 = elem("H1", sect1, pages[0], 0)
shown_p = elem("P", sect1, pages[0], 1)
hidden_p = elem("P", sect1, pages[0], 2)
sect1.K = Array([title1, shown_p, hidden_p])

sect2 = elem("Sect", document)
cont = elem("P", sect2, pages[1], 0)
more = elem("P", sect2, pages[1], 1)
sect2.K = Array([cont, more])

sect3 = elem("Sect", document)
title3 = elem("H1", sect3, pages[2], 0)
last = elem("P", sect3, pages[2], 1)
sect3.K = Array([title3, last])

document.K = Array([sect1, sect2, sect3])
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([title1, shown_p, hidden_p]),
                1,
                Array([cont, more]),
                2,
                Array([title3, last]),
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 3

# The named destination the third and fourth items name: a dictionary carrying
# the page destination and the structure destination side by side.
pdf.Root.Names = Dictionary(
    Dests=pdf.make_indirect(
        Dictionary(
            Names=Array(
                [
                    String("third"),
                    Dictionary(
                        D=Array([pages[2], Name.Fit]),
                        SD=Array([title3, Name.Fit]),
                    ),
                ]
            )
        )
    )
)

outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines))
first = pdf.make_indirect(
    Dictionary(
        Title=String("Opening heading"),
        Parent=outlines,
        A=Dictionary(
            S=Name.GoTo,
            D=Array([pages[0], Name.XYZ, 50, 752, None]),
            SD=Array([title1, Name.XYZ, 50, 752, None]),
        ),
    )
)
child = pdf.make_indirect(
    Dictionary(
        Title=String("Second page"),
        Parent=first,
        Dest=Array([cont, Name.Fit]),
    )
)
third = pdf.make_indirect(
    Dictionary(Title=String("Third page"), Parent=outlines, Dest=String("third"))
)
fourth = pdf.make_indirect(
    Dictionary(
        Title=String("Third page again"),
        Parent=outlines,
        A=Dictionary(S=Name.GoTo, D=String("third")),
    )
)
first.First = child
first.Last = child
first.Count = 1
first.Next = third
third.Prev = first
third.Next = fourth
fourth.Prev = third
outlines.First = first
outlines.Last = fourth
outlines.Count = 4

pdf.Root.Outlines = outlines
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([shown, hidden]),
    D=Dictionary(
        Name=String("Default layers"),
        Order=Array([shown, hidden]),
        OFF=Array([hidden]),
    ),
    Configs=Array(
        [Dictionary(Name=String("All layers"), Order=Array([shown, hidden]))]
    ),
)
pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "outlinedest_ua2.pdf", min_version="2.0")
