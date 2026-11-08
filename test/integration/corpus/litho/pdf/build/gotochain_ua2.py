#!/usr/bin/env python3
"""Build gotochain_ua2.pdf: a tagged document whose GoTo actions run after
other actions, through /Next.

ISO 32000-1, 12.6.2, Table 193 lets an action's /Next hold one action or an
array of actions, performed after it, so the actions an owner triggers form a
tree. ISO 14289-2, 8.8 asks every destination inside the document to be a
structure destination (ISO 32000-2, 12.3.2.3), a GoTo action's among them
wherever it runs. Two corpus files chain a GoTo action after another action,
neither tagged, and none writes /Next as an array, loops it back on itself or
shares a chained action between two owners, so this file does, over three
pages that each open with a heading of their own:

  * an outline item whose direct GoTo action for page 1 has a direct /Next
    array: a Named action, then a GoTo action for page 2 written as an object
    of its own;
  * the catalog /OpenAction, a direct GoTo action for page 1 whose /Next is
    that same GoTo action for page 2;
  * a Link on page 1 whose GoTo action for page 3 runs a URI action, then a
    GoTo action for page 2, whose /Next loops back to the first, all three
    objects of their own;
  * a Link on page 1 whose Named action has a /Next written as an array object
    of its own, holding a direct GoTo action for page 3 and a direct GoTo
    action whose /SD already names page 1's heading;
  * page 2's /AA /O, a Named action written as an object of its own whose
    /Next is a direct GoTo action to (second), a /Names /Dests entry for page 2.

Seven GoTo actions give no structure destination, each failing 8.8 test 2
once: the shared one is one object, judged once. The one carrying /SD passes.

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
        BaseFont=Name("/LithoGoToChain"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoGoToChain"),
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
    marked(b"H1", 0, 740, b"BAAA") + marked(b"P", 1, 700, b"BBAA"),
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


sections = []
headings = []
paragraphs = []
for page in pages:
    sect = elem("Sect", document)
    heading = elem("H1", sect, page, 0)
    para = elem("P", sect, page, 1)
    sections.append(sect)
    headings.append(heading)
    paragraphs.append(para)


def goto(page, *view, **extra):
    return Dictionary(S=Name.GoTo, D=Array([page] + list(view)), **extra)


shared = pdf.make_indirect(goto(pages[1], Name.Fit))

loop_first = pdf.make_indirect(goto(pages[2], Name.Fit))
loop_uri = pdf.make_indirect(Dictionary(S=Name.URI, URI=String("https://example.com/")))
loop_last = pdf.make_indirect(goto(pages[1], Name.FitH, 700))
loop_first.Next = loop_uri
loop_uri.Next = loop_last
loop_last.Next = loop_first

later = pdf.make_indirect(
    Array(
        [
            goto(pages[2], Name.Fit),
            goto(pages[0], Name.Fit, SD=Array([headings[0], Name.Fit])),
        ]
    )
)


def appearance():
    ap = Stream(pdf, b"0 0 1 RG 0 0 40 20 re S\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 40, 20])
    return pdf.make_indirect(ap)


def link(y, action):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Link,
            Rect=Array([50, y, 90, y + 20]),
            F=4,
            Contents=String("a link"),
            AP=Dictionary(N=appearance()),
            A=action,
        )
    )


loop_link = link(600, loop_first)
named_link = link(560, Dictionary(S=Name.Named, N=Name.NextPage, Next=later))
link_elems = []
for key, lk in enumerate([loop_link, named_link], start=len(pages)):
    lk.StructParent = key
    link_elems.append(
        elem(
            "Link",
            sections[0],
            pages[0],
            Array([Dictionary(Type=Name.OBJR, Obj=lk)]),
        )
    )
pages[0].Annots = Array([loop_link, named_link])

sections[0].K = Array([headings[0], paragraphs[0]] + link_elems)
for sect, heading, para in zip(sections[1:], headings[1:], paragraphs[1:]):
    sect.K = Array([heading, para])

pages[1].AA = Dictionary(
    O=pdf.make_indirect(
        Dictionary(
            S=Name.Named,
            N=Name.FirstPage,
            Next=Dictionary(S=Name.GoTo, D=String("second")),
        )
    )
)

document.K = Array(sections)
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
nums = []
for i in range(len(pages)):
    nums += [i, Array([headings[i], paragraphs[i]])]
for key, le in enumerate(link_elems, start=len(pages)):
    nums += [key, le]
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array(nums)))
struct_root.ParentTreeNextKey = len(pages) + len(link_elems)

pdf.Root.Names = Dictionary(
    Dests=pdf.make_indirect(
        Dictionary(
            Names=Array(
                [String("second"), Array([pages[1], Name.XYZ, 50, 752, None])]
            )
        )
    )
)

outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines))
item = pdf.make_indirect(
    Dictionary(
        Title=String("First page"),
        Parent=outlines,
        A=goto(
            pages[0],
            Name.Fit,
            Next=Array([Dictionary(S=Name.Named, N=Name.NextPage), shared]),
        ),
    )
)
outlines.First = item
outlines.Last = item
outlines.Count = 1

pdf.Root.OpenAction = goto(pages[0], Name.XYZ, None, None, 0, Next=shared)
pdf.Root.Outlines = outlines
pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "gotochain_ua2.pdf", min_version="2.0")
