#!/usr/bin/env python3
"""Build headingart_ua2.pdf: generic H headings nested in Art and Sect elements,
under PDF/UA-2.

ISO 14289-2, 8.2.5.12 forbids the H structure type. ISO 32000-1, 14.8.4.3.5
has a strongly structured document's grouping elements nest to reflect its
organization "into articles, sections, subsections", so an Art adds a level as
a Sect does. No corpus file nests an H in a standard Art, so this file does:

  Document
    Art         H, P, Sect
      Sect      H, P
    Art         H, P

Each Art is in the PDF 1.7 namespace, which defines Art; every other element
is in the PDF 2.0 namespace. Each H is the first element of its parent and
the only H there. The three H are the file's one defect: the font is embedded,
the XMP packet claims PDF/UA-2 and every other element is a standard type.

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
        BaseFont=Name("/LithoHeadingArt"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoHeadingArt"),
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
ns17 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF17_NS)))
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))


def elem(s, parent, mcid=None, ns=ns20):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns)
    if mcid is not None:
        d.Pg = page
        d.K = mcid
    return pdf.make_indirect(d)


document = elem("Document", struct_root)
art_first = elem("Art", document, ns=ns17)
sect = elem("Sect", art_first)
art_last = elem("Art", document, ns=ns17)

h_art, p_art = elem("H", art_first, 0), elem("P", art_first, 1)
h_sect, p_sect = elem("H", sect, 2), elem("P", sect, 3)
h_last, p_last = elem("H", art_last, 4), elem("P", art_last, 5)

document.K = Array([art_first, art_last])
art_first.K = Array([h_art, p_art, sect])
sect.K = Array([h_sect, p_sect])
art_last.K = Array([h_last, p_last])

struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20, ns17])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array([0, Array([h_art, p_art, h_sect, p_sect, h_last, p_last])])
    )
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "headingart_ua2.pdf", min_version="2.0")
