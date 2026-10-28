#!/usr/bin/env python3
"""Build remapchain_ua2.pdf: standard structure types role-mapped through a
private type that is itself mapped on to a standard type.

ISO 14289-2, 8.2.4 test 4 reports a standard structure type whose role mapping
leads to a non-standard type. A mapping is followed to its end (ISO 32000-2,
14.8.6), so a chain that passes through a private type and ends on a standard
one leaves the element a standard type. Under a root `Document` in the PDF 2.0
namespace:

  * a `Div` in no namespace, which the root /RoleMap sends to `Section` and
    `Section` on to `Sect`: the legacy chain ends on the standard `Sect`;
  * a `Sect` declaring the PDF 1.7 namespace, whose /RoleMapNS sends it to
    `Section` in a custom namespace, whose own /RoleMapNS sends `Section` to
    the PDF 2.0 `Sect`: the namespace chain ends on a standard type;
  * a `Callout` in the custom namespace, which its /RoleMapNS maps straight to
    the PDF 2.0 `Aside`: a private type correctly mapped, the control.

None of the three fires 8.2.4.

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
PDF17_NS = "http://iso.org/pdf/ssn"
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRemapChain"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRemapChain"),
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

CUSTOM_NS = "http://litho.example/ns/remapchain"

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
custom = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(CUSTOM_NS)))
custom.RoleMapNS = Dictionary(
    Section=Array([Name.Sect, ns20]),
    Callout=Array([Name.Aside, ns20]),
)
ns17 = pdf.make_indirect(
    Dictionary(
        Type=Name.Namespace,
        NS=String(PDF17_NS),
        RoleMapNS=Dictionary(Sect=Array([Name("/Section"), custom])),
    )
)
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
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
div = elem("Div", document, None, Array())
sect_pdf17 = elem("Sect", document, ns17, Array())
callout = elem("Callout", document, custom, Array())
paras = [para(div, 1), para(sect_pdf17, 2), para(callout, 3)]
div.K = Array([paras[0]])
sect_pdf17.K = Array([paras[1]])
callout.K = Array([paras[2]])
document.K = Array([title, div, sect_pdf17, callout])

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, ns17, custom])
struct_root.RoleMap = Dictionary(Div=Name("/Section"), Section=Name.Sect)
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([title] + paras)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "remapchain_ua2.pdf", min_version="2.0")
