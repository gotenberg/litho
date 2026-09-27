#!/usr/bin/env python3
"""Build rolemapns_ua2.pdf: private structure types the root /RoleMap maps,
declared in namespaces that map does not govern.

The structure tree root's legacy /RoleMap governs a type in no namespace, or in
the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
14.8.6). A type declared in any other namespace resolves through that
namespace's /RoleMapNS alone, so ISO 14289-2, 8.2.4 test 1 reports each element
below although the /RoleMap maps its type to a standard one. No corpus file
declares such a namespace, so this file does, one placement per repair a
PDF/UA-2 remediation makes:

  * a `Heading` and a `Para` in a custom namespace that declares no
    /RoleMapNS, which the /RoleMap maps to `H1` and `P`;
  * a `Para` in a second custom namespace whose /RoleMapNS, an indirect
    object, maps only a type no element uses;
  * a `Para` in the PDF 1.7 namespace, which declares a /RoleMapNS of its own
    and so takes itself off the /RoleMap;
  * a `Block` in the PDF 2.0 namespace, which the /RoleMap maps to `P` through
    the private `Body`.

The root `Document` declares the PDF 2.0 namespace. The font is embedded and
draws only `A` and `B`, which is all the text uses.

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
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
PDF17_NS = "http://iso.org/pdf/ssn"
CUSTOM_NS = "http://litho.example/rolemapns/a"
MAPPED_NS = "http://litho.example/rolemapns/b"
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRoleMapNS"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRoleMapNS"),
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
    + marked(b"P", 4, 580, b"AAAB")
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

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
ns17 = pdf.make_indirect(
    Dictionary(
        Type=Name.Namespace,
        NS=String(PDF17_NS),
        RoleMapNS=Dictionary(Chapter=Array([Name.Sect, ns20])),
    )
)
custom = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(CUSTOM_NS)))
mapped = pdf.make_indirect(
    Dictionary(
        Type=Name.Namespace,
        NS=String(MAPPED_NS),
        RoleMapNS=pdf.make_indirect(Dictionary(Other=Array([Name.Span, ns20]))),
    )
)
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, mcid, ns):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem, S=Name("/" + s), NS=ns, P=document, Pg=page, K=mcid
        )
    )


kids = [
    elem("Heading", 0, custom),
    elem("Para", 1, custom),
    elem("Para", 2, mapped),
    elem("Para", 3, ns17),
    elem("Block", 4, ns20),
]
document.K = Array(kids)

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, ns17, custom, mapped])
struct_root.RoleMap = Dictionary(
    Heading=Name.H1, Para=Name.P, Block=Name("/Body"), Body=Name.P
)
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array(kids)])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "rolemapns_ua2.pdf", min_version="2.0")
