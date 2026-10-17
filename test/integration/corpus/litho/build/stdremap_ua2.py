#!/usr/bin/env python3
"""Build stdremap_ua2.pdf: a standard structure type the root /RoleMap
remaps to a private one, on an element declaring the PDF 1.7 namespace.

The structure tree root's legacy /RoleMap governs a type in no namespace, or in
the PDF 1.7 namespace when that namespace declares no /RoleMapNS (ISO 32000-2,
14.8.6). Here it sends `Sect` to `Book`, which no namespace defines and which
nothing maps further, under a root `Document` in the PDF 2.0 namespace:

  * a `Sect` declaring the PDF 1.7 namespace resolves through the map to
    `Book`, so the standard type it names is remapped to a non-standard one:
    ISO 14289-2, 8.2.4 test 4;
  * a `Sect` declaring the PDF 2.0 namespace resolves through that namespace
    alone, so the map does not reach it and it stays the standard `Sect`.

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
        BaseFont=Name("/LithoStdRemap"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoStdRemap"),
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
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def elem(s, parent, ns, k):
    return pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns, K=k)
    )


title = elem("H1", document, ns20, 0)
title.Pg = page
sect_pdf17 = elem("Sect", document, ns17, Array())
sect_pdf20 = elem("Sect", document, ns20, Array())
para_pdf17 = elem("P", sect_pdf17, ns20, 1)
para_pdf17.Pg = page
para_pdf20 = elem("P", sect_pdf20, ns20, 2)
para_pdf20.Pg = page
sect_pdf17.K = Array([para_pdf17])
sect_pdf20.K = Array([para_pdf20])
document.K = Array([title, sect_pdf17, sect_pdf20])

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, ns17])
struct_root.RoleMap = Dictionary(Sect=Name("/Book"))
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([title, para_pdf17, para_pdf20])]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "stdremap_ua2.pdf", min_version="2.0")
