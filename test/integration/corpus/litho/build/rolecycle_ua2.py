#!/usr/bin/env python3
"""Build rolecycle_ua2.pdf: role-map cycles through the root /RoleMap and
through namespaces.

ISO 14289-2, 8.2.4 test 2 forbids a circular mapping. veraPDF 1.30.2 judges it
per structure element, two ways: it follows the root /RoleMap from the
element's type name, whatever namespace the element declares, and reports a
chain that revisits a type; and in a PDF 2.0 document it follows the
/RoleMapNS chain from the element's type in its namespace and reports one that
comes back to a type it left that namespace from. Under a root `Document` in
the PDF 2.0 namespace:

  * a `Sect` in no namespace, which the /RoleMap sends to `Book` and `Book`
    back to `Sect`;
  * a `Callout` in a custom namespace, which its /RoleMapNS sends to `Box` in
    a second custom namespace, whose /RoleMapNS sends `Box` back to the first
    namespace's `Callout`;
  * a `Panel` in a third custom namespace whose /RoleMapNS sends `Panel` to
    its own `Frame` and `Frame` back to `Panel`: a cycle that never leaves the
    namespace, which is test 3's, not test 2's;
  * a PDF 2.0 /RoleMapNS entry mapping `Note` to itself, which no element
    uses.

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
        BaseFont=Name("/LithoRoleCycleNS"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRoleCycleNS"),
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


content = (
    marked(b"H1", 0, 740, b"AABB")
    + marked(b"P", 1, 700, b"ABAB")
    + marked(b"P", 2, 660, b"BABA")
    + marked(b"P", 3, 620, b"BBAA")
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, content),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))
page = pdf.pages[0].obj

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
one = pdf.make_indirect(
    Dictionary(Type=Name.Namespace, NS=String("http://litho.example/ns/cycle-one"))
)
two = pdf.make_indirect(
    Dictionary(Type=Name.Namespace, NS=String("http://litho.example/ns/cycle-two"))
)
three = pdf.make_indirect(
    Dictionary(Type=Name.Namespace, NS=String("http://litho.example/ns/cycle-three"))
)
ns20.RoleMapNS = Dictionary(Note=Array([Name.Note, ns20]))
one.RoleMapNS = Dictionary(Callout=Array([Name("/Box"), two]))
two.RoleMapNS = Dictionary(Box=Array([Name("/Callout"), one]))
three.RoleMapNS = Dictionary(
    Panel=Array([Name("/Frame"), three]),
    Frame=Array([Name("/Panel"), three]),
)

document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, parent, ns, k):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, K=k)
    if ns is not None:
        d.NS = ns
    return pdf.make_indirect(d)


def para(parent, mcid):
    p = elem("P", parent, ns20, mcid)
    p.Pg = page
    return p


title = elem("H1", document, ns20, 0)
title.Pg = page
sect = elem("Sect", document, None, Array())
callout = elem("Callout", document, one, Array())
panel = elem("Panel", document, three, Array())
paras = [para(sect, 1), para(callout, 2), para(panel, 3)]
sect.K = Array([paras[0]])
callout.K = Array([paras[1]])
panel.K = Array([paras[2]])
document.K = Array([title, sect, callout, panel])

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, one, two, three])
struct_root.RoleMap = Dictionary(Sect=Name("/Book"), Book=Name.Sect)
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([title] + paras)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "rolecycle_ua2.pdf", min_version="2.0")
