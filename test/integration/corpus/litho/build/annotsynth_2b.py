#!/usr/bin/env python3
"""Build annotsynth_2b.pdf: annotations with no appearance stream that the
renderer draws from their dictionaries, at PDF/A-2b.

ISO 32000-1, 12.5.6 defines a Polygon (12.5.6.9), an Underline and a Squiggly
(12.5.6.10) by their dictionaries alone, and viewers draw them when the file
holds no appearance stream. The render goldens of the corpora held Square,
Circle, Line, PolyLine, Ink, Highlight, StrikeOut and Link annotations with no
/AP, but no Polygon, Underline or Squiggly, so the render cell compared none
of them. The page draws, in DeviceRGB:

| Annotation | Draws                                                               |
| ---------- | ------------------------------------------------------------------- |
| Polygon    | a triangle filled blue with a red border 3 wide                     |
| Polygon    | a pentagon filled yellow with a dashed border, at /CA 0.5 over a    |
|            | grey bar                                                            |
| Underline  | a red line along the bottom of two quadrilaterals over cyan bars    |
| Squiggly   | a green zig-zag along the bottom of a quadrilateral over a cyan bar |

Each annotation sets /F 4 and lacks the /AP that ISO 19005-2, 6.3.3 requires,
so 6.3.3 t1 fires. The file declares no OutputIntent, so DeviceRGB fails
6.2.4.3 t2, and the half-opaque polygon is no transparency the page content
holds.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

content = (
    b"0.6 0.6 0.6 rg 100 110 90 20 re f\n"
    b"0 1 1 rg 10 60 80 12 re f 10 40 80 12 re f 110 50 80 12 re f\n"
)


def annot(subtype, rect, **entries):
    return pdf.make_indirect(
        Dictionary(Type=Name.Annot, Subtype=Name("/" + subtype), Rect=Array(rect), F=4, **entries)
    )


annots = Array(
    [
        annot(
            "Polygon",
            [10, 100, 90, 190],
            Vertices=Array([15, 105, 85, 105, 50, 185]),
            C=Array([1, 0, 0]),
            IC=Array([0, 0, 1]),
            BS=Dictionary(W=3),
        ),
        annot(
            "Polygon",
            [100, 100, 190, 190],
            Vertices=Array([145, 185, 185, 155, 170, 105, 120, 105, 105, 155]),
            C=Array([0, 0, 0]),
            IC=Array([1, 1, 0]),
            BS=Dictionary(W=2, S=Name.D, D=Array([6, 3])),
            CA=0.5,
        ),
        annot(
            "Underline",
            [10, 40, 90, 72],
            QuadPoints=Array([10, 72, 90, 72, 10, 60, 90, 60, 10, 52, 90, 52, 10, 40, 90, 40]),
            C=Array([1, 0, 0]),
        ),
        annot(
            "Squiggly",
            [110, 50, 190, 62],
            QuadPoints=Array([110, 62, 190, 62, 110, 50, 190, 50]),
            C=Array([0, 0.5, 0]),
        ),
    ]
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 200]),
    Resources=Dictionary(),
    Contents=Stream(pdf, content),
    Annots=annots,
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "annotsynth_2b.pdf", min_version="1.7")
