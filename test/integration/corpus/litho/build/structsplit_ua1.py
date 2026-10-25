#!/usr/bin/env python3
"""Build structsplit_ua1.pdf: structure elements a redaction cuts.

A region redaction removes the glyphs it covers and keeps the BDC and EMC
around them, so a structure element whose marked content the region crosses
keeps tagging what survived (ISO 32000-1, 14.7.4.2). What a cut cannot divide
is the text an element holds itself: /ActualText, /Alt and /E describe the
element's whole content, its descendants' included (14.9.3 to 14.9.5).

Page 1 places every element the centred quarter of the page crosses, and none
of them holds text the cut divides:

* a paragraph, MCID 1, one line whose middle words the quarter takes;
* a table row whose second cell, MCID 3, lies inside the quarter while its
  first, MCID 2, lies outside;
* a paragraph of two spans under a division, the first span, MCID 4, inside
  the quarter and the second, MCID 5, outside;
* a division whose /ActualText describes one span, MCID 6, wholly inside the
  quarter, which the redaction clears as it removes the span.

Page 2 is the refusal that stays: one paragraph, MCID 0, holding its words in
/ActualText, whose line the centred quarter cuts.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def run(mcid, tag, x, y, text, size=12):
    """One marked-content sequence drawing text at (x, y)."""
    return b"/%s <</MCID %d>> BDC BT /F1 %d Tf %d %d Td (%s) Tj ET EMC\n" % (
        tag.encode(),
        mcid,
        size,
        x,
        y,
        text,
    )


page1_content = b"".join(
    [
        run(0, "H1", 72, 720, b"Structure split", 18),
        run(1, "P", 72, 500, b"Kept before the cut, then these removed middle words, and the words kept after the cut"),
        run(2, "TD", 72, 420, b"Name"),
        run(3, "TD", 300, 420, b"Hidden value"),
        run(4, "Span", 200, 340, b"inner span"),
        run(5, "Span", 470, 340, b"outer span"),
        run(6, "Span", 200, 260, b"inner words"),
    ]
)
page2_content = run(0, "P", 72, 396, b"Left side words SECRET right side words")

pages = []
for number, data in enumerate([page1_content, page2_content]):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(Font=Dictionary(F1=font)),
            Contents=Stream(pdf, data),
            StructParents=number,
        )
    )
    pdf.pages.append(pikepdf.Page(page))
    pages.append(page)
page1, page2 = pages

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))


def elem(tag, parent, kids, page, **extra):
    """A structure element of page, its /K set to kids."""
    e = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name(tag), P=parent, Pg=page, **extra))
    e.K = kids
    return e


heading = elem("/H1", document, 0, page1)
para = elem("/P", document, 1, page1)
table = elem("/Table", document, Array(), page1)
row = elem("/TR", table, Array(), page1)
cell_kept = elem("/TD", row, 2, page1)
cell_cut = elem("/TD", row, 3, page1)
row.K = Array([cell_kept, cell_cut])
table.K = Array([row])

nest = elem("/Div", document, Array(), page1)
nest_para = elem("/P", nest, Array(), page1)
span_in = elem("/Span", nest_para, 4, page1)
span_out = elem("/Span", nest_para, 5, page1)
nest_para.K = Array([span_in, span_out])
nest.K = Array([nest_para])

described = elem("/Div", document, Array(), page1, ActualText=String("inner words"))
described_para = elem("/P", described, Array(), page1)
span_whole = elem("/Span", described_para, 6, page1)
described_para.K = Array([span_whole])
described.K = Array([described_para])

spanning = elem(
    "/P", document, 0, page2, ActualText=String("Left side words SECRET right side words")
)

document.K = Array([heading, para, table, nest, described, spanning])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([heading, para, cell_kept, cell_cut, span_in, span_out, span_whole]),
                1,
                Array([spanning]),
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 2

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "structsplit_ua1.pdf", min_version="1.7")
