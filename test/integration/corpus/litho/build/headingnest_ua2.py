#!/usr/bin/env python3
"""Build headingnest_ua2.pdf: generic H headings nested in Sect elements, under
PDF/UA-2.

ISO 14289-2, 8.2.5.12 forbids the H structure type. ISO 32000-1, Table 335
defines H as the label of the division it is the first child of, whose level a
reader takes from the nesting of the sections. No corpus file nests an H more
than one Sect deep, so this file does:

  Document
    Sect        H, P, Sect
      Sect      H, P, Sect
        Sect    H, P
    Sect        H, P

Each H is the first element of its Sect and the only H there. The four H
are the file's one defect: the font is embedded, the XMP packet claims PDF/UA-2
and every other element is a standard PDF 2.0 type.

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
LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoHeadingNest"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoHeadingNest"),
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

# The marked-content tags name the structure type each sequence ends up under,
# which veraPDF does not judge.
TEXT = [
    (b"H", b"AABB"),
    (b"P", b"ABAB"),
    (b"H", b"BABA"),
    (b"P", b"BBAA"),
    (b"H", b"ABBA"),
    (b"P", b"BAAB"),
    (b"H", b"AAAB"),
    (b"P", b"BBBA"),
]
content = b"".join(
    b"/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n"
    % (tag, mcid, 740 - mcid * 40, s)
    for mcid, (tag, s) in enumerate(TEXT)
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
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))


def elem(s, parent, mcid=None):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns20)
    if mcid is not None:
        d.Pg = page
        d.K = mcid
    return pdf.make_indirect(d)


document = elem("Document", struct_root)
sect_outer = elem("Sect", document)
sect_mid = elem("Sect", sect_outer)
sect_inner = elem("Sect", sect_mid)
sect_last = elem("Sect", document)

h_outer, p_outer = elem("H", sect_outer, 0), elem("P", sect_outer, 1)
h_mid, p_mid = elem("H", sect_mid, 2), elem("P", sect_mid, 3)
h_inner, p_inner = elem("H", sect_inner, 4), elem("P", sect_inner, 5)
h_last, p_last = elem("H", sect_last, 6), elem("P", sect_last, 7)

document.K = Array([sect_outer, sect_last])
sect_outer.K = Array([h_outer, p_outer, sect_mid])
sect_mid.K = Array([h_mid, p_mid, sect_inner])
sect_inner.K = Array([h_inner, p_inner])
sect_last.K = Array([h_last, p_last])

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array(
                    [h_outer, p_outer, h_mid, p_mid, h_inner, p_inner, h_last, p_last]
                ),
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "headingnest_ua2.pdf", min_version="2.0")
