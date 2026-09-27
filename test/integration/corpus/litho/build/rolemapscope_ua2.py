#!/usr/bin/env python3
"""Build rolemapscope_ua2.pdf: private structure types the root /RoleMap maps
to standard ones, read through the namespace each element declares.

The structure tree root's legacy /RoleMap governs a type in no namespace, or in
the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
14.8.6). A type an element declares in the PDF 2.0 namespace resolves through
that namespace alone, and the PDF 2.0 namespace defines no private type. No
corpus file declares a namespace on an element whose type only the legacy map
maps, so this file does, beside the two placements the map does reach:

  * the root's single child is `Workbook`, in no namespace, which the /RoleMap
    maps to `Document`: the PDF 1.7 Document, so ISO 14289-2, 8.2.5.2 test 2
    reports a root Document outside the PDF 2.0 namespace. Remediation to
    PDF/UA-2 names it a PDF 2.0 Document;
  * a `Para` declaring the PDF 2.0 namespace, which the /RoleMap maps to `P`
    and which no mapping reaches there: ISO 14289-2, 8.2.4 test 1;
  * a `Para` in no namespace and one declaring the PDF 1.7 namespace, both
    mapped to `P` by the /RoleMap, which must not fire.

The font is embedded and draws only `A` and `B`, which is all the text uses.

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
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRoleMapScope"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRoleMapScope"),
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

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
ns17 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF17_NS)))
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
workbook = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name("/Workbook"), P=struct_root)
)


def elem(s, mcid, ns=None):
    d = Dictionary(
        Type=Name.StructElem, S=Name("/" + s), P=workbook, Pg=page, K=mcid
    )
    if ns is not None:
        d.NS = ns
    return pdf.make_indirect(d)


title = elem("H1", 0, ns20)
para_pdf2 = elem("Para", 1, ns20)
para_none = elem("Para", 2)
para_pdf17 = elem("Para", 3, ns17)
workbook.K = Array([title, para_pdf2, para_none, para_pdf17])

struct_root.K = Array([workbook])
struct_root.Namespaces = Array([ns20, ns17])
struct_root.RoleMap = Dictionary(Workbook=Name.Document, Para=Name.P)
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([title, para_pdf2, para_none, para_pdf17])]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "rolemapscope_ua2.pdf", min_version="2.0")
