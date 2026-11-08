#!/usr/bin/env python3
"""Build destforms_ua2.pdf: a tagged document whose destinations take the
forms PDF/UA-2 remediation gives a structure destination beyond a plain
destination array.

ISO 14289-2, 8.8 asks every destination inside the document to be a structure
destination (ISO 32000-2, 12.3.2.3). A named destination is judged by the value
it maps to, and a GoTo action wherever it hangs, additional actions included.
No corpus file reaches a GoTo action under a page's /AA, or a direct one under
an annotation's /AA, and none that the gate remediates to PDF/UA-2 names a
destination through the catalog /Dests or writes a dictionary under /Dest, so
this file does, over three pages that each open with a heading or a paragraph
of their own:

  * an outline item whose /Dest is (second), a /Names /Dests entry for page 2;
  * an outline item whose GoTo action's /D is /third, a catalog /Dests entry
    for page 3 (ISO 32000-1, 12.3.2.3);
  * a Link on page 1 whose /Dest is /third and whose direct /AA holds a direct
    GoTo action for page 1 under /E;
  * a Link on page 1 whose /Dest is a GoTo action dictionary for page 2, which
    ISO 32000-1, Table 173 does not allow and readers follow all the same, and
    whose /AA, an object of its own, holds a GoTo action to (second) under /X;
  * page 2's /AA, whose /O is a direct GoTo action for page 2 and whose /C is a
    GoTo action for page 3 written as an object of its own (Table 195).

No destination names a structure element, so each fails 8.8: test 1 for the
first item and the two links' /Dest, test 2 for the five GoTo actions.

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
        BaseFont=Name("/LithoDestForms"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoDestForms"),
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


def marked(tag, mcid, y, s):
    return b"/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n" % (
        tag,
        mcid,
        y,
        s,
    )


contents = [
    marked(b"H1", 0, 740, b"AAAB") + marked(b"P", 1, 700, b"AABA"),
    marked(b"P", 0, 740, b"BAAA") + marked(b"P", 1, 700, b"BBAA"),
    marked(b"H1", 0, 740, b"BABA") + marked(b"P", 1, 700, b"BBBA"),
]
pages = []
for i, content in enumerate(contents):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(Font=Dictionary(F1=font)),
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
intro = elem("P", sect1, pages[0], 1)

sect2 = elem("Sect", document)
cont = elem("P", sect2, pages[1], 0)
more = elem("P", sect2, pages[1], 1)
sect2.K = Array([cont, more])

sect3 = elem("Sect", document)
title3 = elem("H1", sect3, pages[2], 0)
last = elem("P", sect3, pages[2], 1)
sect3.K = Array([title3, last])


def appearance():
    ap = Stream(pdf, b"0 0 1 RG 0 0 40 20 re S\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 40, 20])
    return pdf.make_indirect(ap)


def link(y, **entries):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Link,
            Rect=Array([50, y, 90, y + 20]),
            F=4,
            Contents=String("a link"),
            AP=Dictionary(N=appearance()),
            **entries,
        )
    )


named_link = link(
    600,
    Dest=Name("/third"),
    AA=Dictionary(E=Dictionary(S=Name.GoTo, D=Array([pages[0], Name.Fit]))),
)
dict_link = link(
    560,
    Dest=Dictionary(Type=Name.Action, S=Name.GoTo, D=Array([pages[1], Name.Fit])),
    AA=pdf.make_indirect(Dictionary(X=Dictionary(S=Name.GoTo, D=String("second")))),
)
link_elems = []
for key, lk in enumerate([named_link, dict_link], start=len(pages)):
    lk.StructParent = key
    link_elems.append(
        elem("Link", sect1, pages[0], Array([Dictionary(Type=Name.OBJR, Obj=lk)]))
    )
pages[0].Annots = Array([named_link, dict_link])
sect1.K = Array([title1, intro] + link_elems)

pages[1].AA = Dictionary(
    O=Dictionary(S=Name.GoTo, D=Array([pages[1], Name.Fit])),
    C=pdf.make_indirect(Dictionary(S=Name.GoTo, D=Array([pages[2], Name.Fit]))),
)

document.K = Array([sect1, sect2, sect3])
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([title1, intro]),
                1,
                Array([cont, more]),
                2,
                Array([title3, last]),
                3,
                link_elems[0],
                4,
                link_elems[1],
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 5

pdf.Root.Names = Dictionary(
    Dests=pdf.make_indirect(
        Dictionary(
            Names=Array(
                [String("second"), Array([pages[1], Name.XYZ, 50, 752, None])]
            )
        )
    )
)
pdf.Root.Dests = Dictionary(third=Array([pages[2], Name.Fit]))

outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines))
first = pdf.make_indirect(
    Dictionary(Title=String("Second page"), Parent=outlines, Dest=String("second"))
)
second = pdf.make_indirect(
    Dictionary(
        Title=String("Third page"),
        Parent=outlines,
        Prev=first,
        A=Dictionary(S=Name.GoTo, D=Name("/third")),
    )
)
first.Next = second
outlines.First = first
outlines.Last = second
outlines.Count = 2

pdf.Root.Outlines = outlines
pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "destforms_ua2.pdf", min_version="2.0")
