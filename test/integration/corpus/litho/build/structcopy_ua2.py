#!/usr/bin/env python3
"""Build structcopy_ua2.pdf: a tagged document whose structure tree reaches
every part a copy of its pages carries.

Split, merge and page insertion carry the structure elements that describe the
pages they copy, and the entries of the structure tree root that name those
elements. No corpus file packs all of those entries into one tree over several
pages, so this file does, each one reached by content on a page a copy keeps
or leaves out:

  * a root `/RoleMap` mapping the custom type `Para` to `P` for the elements
    in the default namespace (ISO 32000-1, 14.7.3);
  * a `/ClassMap` defining the `Centered` class one paragraph names (14.7.5.2);
  * an `/IDTree` listing the identifiers of a heading, two paragraphs and the
    two header cells whose identifiers the data cells name in `/Headers`
    (Table 323, Table 349);
  * two `/Namespaces`: the PDF 2.0 one the Document element names, and a
    custom one whose `/RoleMapNS` maps `Callout` to the PDF 2.0 `Aside`
    (ISO 32000-2, 14.7.4.2);
  * a Link element on page 1 and one on page 3, each enclosing its link
    annotation through an object reference (14.7.4.3), whose `/StructParent`
    keys the `/ParentTree` after the pages' `/StructParents`;
  * a `Sect` naming no page whose paragraphs sit on pages 1 and 2, so a copy
    of either page keeps part of it, and a table wholly on page 2.

Each page opens with content its own elements describe, and the headings are
`H1` on pages 1 and 3, so moving any page to the end leaves a reading order
PDF/UA accepts: `mutate-pages` moves page 1 behind page 3, and the copy carries
its elements. The font is embedded and draws only `A` and `B`, which is all the
text uses, so no font rule masks the structure.

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
CUSTOM_NS = "http://litho.example/structcopy"
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoStructCopy"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoStructCopy"),
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
    return (
        b"/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n"
        % (tag, mcid, y, s)
    )


def link_annot(y, uri, alt):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Link,
            Rect=Array([50, y - 4, 150, y + 14]),
            Border=Array([0, 0, 0]),
            F=4,
            Contents=String(alt),
            A=Dictionary(S=Name.URI, URI=String(uri)),
        )
    )


link1 = link_annot(600, "https://example.com/one", "first link")
link3 = link_annot(600, "https://example.com/three", "third link")
link1.StructParent = 3
link3.StructParent = 4

# No word of page 1 occurs anywhere else, so a redaction of the page has no
# copy left to refuse over.
contents = [
    marked(b"H1", 0, 740, b"AAAAAB")
    + marked(b"P", 1, 700, b"AAAABB")
    + marked(b"P", 2, 660, b"AAABBB")
    + marked(b"Link", 3, 600, b"AABBBB"),
    marked(b"P", 0, 740, b"BBBA")
    + marked(b"TH", 1, 700, b"BA")
    + marked(b"TH", 2, 680, b"BB")
    + marked(b"TD", 3, 660, b"BAB")
    + marked(b"TD", 4, 640, b"BBA"),
    marked(b"H1", 0, 740, b"BABA")
    + marked(b"P", 1, 700, b"BAAB")
    + marked(b"P", 2, 660, b"BBBB")
    + marked(b"Link", 3, 600, b"BABB"),
]
annots = [[link1], [], [link3]]
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
    if annots[i]:
        page.Annots = Array(annots[i])
    pdf.pages.append(pikepdf.Page(page))
    pages.append(pdf.pages[i].obj)
link1.P = pages[0]
link3.P = pages[2]

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
custom_ns = pdf.make_indirect(
    Dictionary(
        Type=Name.Namespace,
        NS=String(CUSTOM_NS),
        RoleMapNS=Dictionary(Callout=Array([Name.Aside, ns20])),
    )
)
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, parent, page=None, k=None, **extra):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent)
    if page is not None:
        d.Pg = page
    if k is not None:
        d.K = k
    for key, value in extra.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


# Page 1 and the start of page 2, under a Sect naming no page.
sect1 = elem("Sect", document)
title1 = elem("H1", sect1, pages[0], 0, ID=String("title-1"))
intro = elem("Para", sect1, pages[0], 1, ID=String("intro"))
centered = elem("Para", sect1, pages[0], 2, C=Name("/Centered"))
link1_elem = elem(
    "Link",
    sect1,
    pages[0],
    Array([3, Dictionary(Type=Name.OBJR, Obj=link1)]),
    Alt=String("first link"),
)
cont = elem("Para", sect1, pages[1], 0, ID=String("cont"))
sect1.K = Array([title1, intro, centered, link1_elem, cont])

# The table, wholly on page 2.
table = elem("Table", document, pages[1])
head_row = elem("TR", table, pages[1])
body_row = elem("TR", table, pages[1])
th1 = elem(
    "TH",
    head_row,
    pages[1],
    1,
    ID=String("h1"),
    A=Dictionary(O=Name.Table, Scope=Name.Column),
)
th2 = elem(
    "TH",
    head_row,
    pages[1],
    2,
    ID=String("h2"),
    A=Dictionary(O=Name.Table, Scope=Name.Column),
)
td1 = elem(
    "TD",
    body_row,
    pages[1],
    3,
    A=Dictionary(O=Name.Table, Headers=Array([String("h1")])),
)
td2 = elem(
    "TD",
    body_row,
    pages[1],
    4,
    A=Dictionary(O=Name.Table, Headers=Array([String("h2")])),
)
head_row.K = Array([th1, th2])
body_row.K = Array([td1, td2])
table.K = Array([head_row, body_row])

# Page 3: a French paragraph and a callout in the custom namespace.
sect3 = elem("Sect", document, pages[2])
title3 = elem("H1", sect3, pages[2], 0)
french = elem("Para", sect3, pages[2], 1, Lang=String("fr-FR"))
callout = elem("Callout", sect3, pages[2], NS=custom_ns)
aside_p = elem("P", callout, pages[2], 2)
callout.K = Array([aside_p])
link3_elem = elem(
    "Link",
    sect3,
    pages[2],
    Array([3, Dictionary(Type=Name.OBJR, Obj=link3)]),
    Alt=String("third link"),
)
sect3.K = Array([title3, french, callout, link3_elem])

document.K = Array([sect1, table, sect3])
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, custom_ns])
struct_root.RoleMap = Dictionary(Para=Name.P)
struct_root.ClassMap = Dictionary(
    Centered=Dictionary(O=Name.Layout, TextAlign=Name.Center)
)
struct_root.IDTree = pdf.make_indirect(
    Dictionary(
        Names=Array(
            [
                String("cont"),
                cont,
                String("h1"),
                th1,
                String("h2"),
                th2,
                String("intro"),
                intro,
                String("title-1"),
                title1,
            ]
        )
    )
)
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([title1, intro, centered, link1_elem]),
                1,
                Array([cont, th1, th2, td1, td2]),
                2,
                Array([title3, french, aside_p, link3_elem]),
                3,
                link1_elem,
                4,
                link3_elem,
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 5

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "structcopy_ua2.pdf", min_version="2.0")
