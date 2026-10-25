#!/usr/bin/env python3
"""Build annotstraddle_2b.pdf: an annotation reaching outside the regions
redaction selects it by, and the pop-up and reply of one it removes, at
PDF/A-2b.

An annotation is atomic (ISO 32000-1, 12.5.2), so redaction refuses one a
region meets without containing. The raster fallback draws such an
annotation into the page as a viewer draws it (12.5.5, Algorithm 8.1) and
removes it, so its look is page content the region cuts. A pop-up names its
parent by /Parent (12.5.6.14) and a reply names the annotation it answers by
/IRT (12.5.6.2): redaction removed an annotation from /Annots and left both,
and the rewrite wrote the removed annotation through them. Both pages are 200
by 200 and draw in DeviceGray:

| Page | Annotation | /Rect              | Draws, and what names it                          |
| ---- | ---------- | ------------------ | ------------------------------------------------- |
| 1    | Square     | [120 60 180 120]   | a dark square, across the quarter and the band    |
| 1    | Popup      | [150 160 195 195]  | nothing, closed; /Parent the Square               |
| 2    | Ink        | [70 70 130 130]    | a stroke, inside the centred quarter              |
| 2    | Popup      | [155 155 195 195]  | nothing, closed; /Parent the Ink                  |
| 2    | Text       | [10 10 34 34]      | a small square; /IRT the Ink, /RT /R              |

Page 1 also draws a grey bar across the middle. Every annotation but the
pop-ups sets /F 4 and has a normal appearance stream.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def form(bbox, data):
    return pdf.make_indirect(
        Stream(pdf, data, Type=Name.XObject, Subtype=Name.Form, BBox=Array(bbox), Resources=Dictionary())
    )


def annot(subtype, rect, **entries):
    return pdf.make_indirect(
        Dictionary(Type=Name.Annot, Subtype=Name("/" + subtype), Rect=Array(rect), **entries)
    )


square = annot(
    "Square",
    [120, 60, 180, 120],
    F=4,
    Contents=pikepdf.String("Square comment"),
    AP=Dictionary(N=form([0, 0, 60, 60], b"0.2 g 0 0 60 60 re f\n")),
)
square_popup = annot("Popup", [150, 160, 195, 195], Parent=square)
square.Popup = square_popup

ink = annot(
    "Ink",
    [70, 70, 130, 130],
    F=4,
    InkList=Array([Array([75, 75, 100, 125, 125, 75])]),
    Contents=pikepdf.String("Ink comment"),
    AP=Dictionary(N=form([0, 0, 60, 60], b"0 G 2 w 5 5 m 30 55 l 55 5 l S\n")),
)
ink_popup = annot("Popup", [155, 155, 195, 195], Parent=ink)
ink.Popup = ink_popup
reply = annot(
    "Text",
    [10, 10, 34, 34],
    F=4,
    IRT=ink,
    RT=Name.R,
    Contents=pikepdf.String("Reply"),
    AP=Dictionary(N=form([0, 0, 24, 24], b"0.5 g 2 2 20 20 re f\n")),
)

page1 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 200]),
    Resources=Dictionary(),
    Contents=Stream(pdf, b"0.6 g 20 90 160 20 re f\n"),
    Annots=Array([square, square_popup]),
)
page2 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 200]),
    Resources=Dictionary(),
    Contents=Stream(pdf, b"0.8 g 20 160 40 20 re f\n"),
    Annots=Array([ink, ink_popup, reply]),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page1)))
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page2)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "annotstraddle_2b.pdf", min_version="1.7")
