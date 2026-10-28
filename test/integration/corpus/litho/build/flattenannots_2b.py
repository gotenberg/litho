#!/usr/bin/env python3
"""Build flattenannots_2b.pdf: annotations that paint with transparency, which
transparency flattening draws into the page, at PDF/A-2b.

Writer.FlattenTransparency draws into the page content the appearance of each
annotation that paints with transparency, leaves the annotation with an
appearance that paints nothing, and flattens the page. The corpora hold such
annotations, but the flatten cell's render sample reaches few kinds of them,
none synthesised from its dictionary, no annotation drawn under a transparent
one, and no stroked straight line, whose path box has no area. Every glyph is
a Type 3 glyph painting in the text's colour, with a /ToUnicode map, so the
text extracts and draws the same in every renderer:

| Page | /Annots, in order                                                     |
| ---- | --------------------------------------------------------------------- |
| 1    | an opaque Circle the Highlight overlaps, a Multiply Highlight over    |
|      | text with a closed Popup, a Square at /CA 0.5 with an opaque          |
|      | appearance, a Square at /CA 0.5 with no appearance, an opaque Ink     |
|      | apart from the rest, and a hidden Square at /CA 0.5                   |
| 2    | a FreeText whose background paints at ca 0.6 under opaque text        |
| 3    | an Ink annotation stroking a straight line at ca 0.7 below text       |
| 4    | the same Ink line through the text, and a FreeText whose text paints  |
|      | at ca 0.6 too                                                         |

What flattening makes of each: the Highlight, both Squares, the Ink line and
the FreeText paint with transparency and are drawn into the page, the Square
with no appearance as litho synthesises it; the Circle is drawn too, before
the Highlight, because a viewer drew it under the Highlight; the Popup stays,
and so does the opaque Ink no drawn annotation overlaps; the hidden Square
loses its appearance with nothing drawn. The FreeText's text joins the page's.
The flatten cell renders the first three pages, which flattening draws
exactly. Page 4 holds what it approximates and reports: the text the Ink line
crosses draws above it untinted (TextUnderTransparency), and the FreeText's
transparent text draws opaque, composited over white (TransparentText).

The file declares no OutputIntent and its pages no /Group, so DeviceRGB fails
ISO 19005-2, 6.2.4.3 t2 and every page holding transparency 6.2.10 t2; the
appearance-less Square fails 6.3.3 t1 and the hidden one 6.3.2 t2.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, tounicode_cmap, type3_font, xmp_packet

pdf = pikepdf.new()

GLYPHS = [
    ("A", 600, "0 0 500 700 re f"),
    ("B", 600, "0 0 500 700 re 100 100 300 500 re f*"),
    ("C", 600, "0 0 m 500 0 l 250 700 l f"),
    ("D", 600, "0 300 500 150 re f 175 0 150 700 re f"),
    ("E", 600, "200 0 120 700 re f 0 580 500 120 re f"),
]
font = type3_font(pdf, GLYPHS, [600] * len(GLYPHS))
font.ToUnicode = tounicode_cmap(pdf, {65 + i: ord(name) for i, (name, _, _) in enumerate(GLYPHS)})
font = pdf.make_indirect(font)


def form(content, bbox, resources=None, matrix=None):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array(bbox)
    if matrix is not None:
        stream.Matrix = Array(matrix)
    stream.Resources = resources if resources is not None else Dictionary()
    return pdf.make_indirect(stream)


def state(**entries):
    return pdf.make_indirect(Dictionary(Type=Name.ExtGState, **entries))


def annot(subtype, rect, **entries):
    return pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=subtype, Rect=Array(rect), **entries))


def add_page(content, annots):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 200, 100]),
            Resources=Dictionary(Font=Dictionary(F=font)),
            Contents=Stream(pdf, content),
        )
    )
    for a in annots:
        a.P = page
    page.Annots = Array(annots)
    pdf.pages.append(pikepdf.Page(page))


multiply = state(BM=Name.Multiply)
circle = annot(
    Name.Circle,
    [5, 30, 45, 70],
    F=4,
    C=Array([0, 0.6, 0]),
    AP=Dictionary(N=form(b"0 0.6 0 RG 2 w 20 2 m 38 20 l 20 38 l 2 20 l h S\n", [0, 0, 40, 40])),
)
highlight = annot(
    Name.Highlight,
    [8, 36, 112, 64],
    F=4,
    C=Array([1, 1, 0]),
    QuadPoints=Array([8, 64, 112, 64, 8, 36, 112, 36]),
    Contents=String("Check this"),
    AP=Dictionary(N=form(b"/R0 gs 1 1 0 rg 0 0 104 28 re f\n", [8, 36, 112, 64], Dictionary(ExtGState=Dictionary(R0=multiply)), [1, 0, 0, 1, -8, -36])),
)
popup = annot(Name.Popup, [120, 60, 190, 95], Parent=highlight, Open=False)
highlight.Popup = popup
faded = annot(
    Name.Square,
    [130, 10, 160, 40],
    F=4,
    CA=0.5,
    C=Array([0, 0, 1]),
    AP=Dictionary(N=form(b"0 0 1 rg 0 0 30 30 re f\n", [0, 0, 30, 30])),
)
drawn = annot(Name.Square, [165, 10, 195, 40], F=4, CA=0.5, C=Array([1, 0, 0]), IC=Array([1, 0.8, 0.8]), Border=Array([0, 0, 2]))
ink = annot(
    Name.Ink,
    [170, 60, 195, 95],
    F=4,
    C=Array([0.5, 0, 0.5]),
    InkList=Array([Array([172, 62, 193, 93])]),
    AP=Dictionary(N=form(b"0.5 0 0.5 RG 2 w 2 2 m 23 33 l S\n", [0, 0, 25, 35])),
)
hidden = annot(
    Name.Square,
    [60, 5, 90, 30],
    F=6,
    CA=0.5,
    C=Array([1, 0, 0]),
    AP=Dictionary(N=form(b"1 0 0 rg 0 0 30 25 re f\n", [0, 0, 30, 25])),
)
add_page(
    b"0.85 g 0 0 200 100 re f BT /F 20 Tf 0 g 10 40 Td (ABCDE) Tj ET\n",
    [circle, highlight, popup, faded, drawn, ink, hidden],
)



def ink_line(y, rect):
    return annot(
        Name.Ink,
        rect,
        F=4,
        CA=0.7,
        C=Array([1, 0, 0]),
        InkList=Array([Array([10, y, 190, y])]),
        AP=Dictionary(N=form(b"/X gs 1 0 0 RG 4 w 1 J 5 5 m 185 5 l S\n", [0, 0, 190, 10], Dictionary(ExtGState=Dictionary(X=state(CA=0.7, ca=0.7))))),
    )


def free_text(content):
    return annot(
        Name.FreeText,
        [120, 60, 190, 90],
        F=4,
        Contents=String("DEA"),
        DA=String("0 0 1 rg /F 12 Tf"),
        AP=Dictionary(N=form(content, [0, 0, 70, 30], Dictionary(Font=Dictionary(F=font), ExtGState=Dictionary(T=state(ca=0.6))))),
    )


add_page(
    b"BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n",
    [free_text(b"q /T gs 0.8 0.9 1 rg 0 0 70 30 re f Q BT /F 16 Tf 0 0 1 rg 8 9 Td (DEA) Tj ET\n")],
)
add_page(b"BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n", [ink_line(15, [5, 10, 195, 20])])
add_page(
    b"BT /F 20 Tf 0 g 10 38 Td (ABCDEABCDE) Tj ET\n",
    [ink_line(45, [5, 40, 195, 50]), free_text(b"/T gs 0.8 0.9 1 rg 0 0 70 30 re f BT /F 16 Tf 0 0 1 rg 8 9 Td (DEA) Tj ET\n")],
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "flattenannots_2b.pdf", min_version="1.7")
