#!/usr/bin/env python3
"""Build annotdrawn_2b.pdf: annotations with no appearance stream drawn where
their /Rect does not sit, at PDF/A-2b.

A viewer draws an annotation with no appearance stream from its dictionary
where ISO 32000-1, 12.5.6 defines how: a Line from /L (12.5.6.7), a PolyLine
from /Vertices (12.5.6.9), wherever /Rect says it is. Redaction placed an
annotation by /Rect alone, so a region crossed by such a drawing kept the
annotation and the page went on showing it inside the region. The page draws,
in DeviceRGB:

| Annotation | /Rect               | Draws                                         |
| ---------- | ------------------- | --------------------------------------------- |
| Line       | [0 0 0 0]           | a red line 2 wide across the page at y 100    |
| PolyLine   | [170 170 190 190]   | a blue zig-zag 2 wide along the bottom edge   |

A grey bar under the centre gives the page content to redact. Both
annotations set /F 4 and lack an /AP. ISO 19005-2, 6.3.3 exempts the Line,
whose /Rect has no area, and 6.3.3 t1 fires for the PolyLine. The file
declares no OutputIntent, so DeviceRGB fails 6.2.4.3 t2.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

content = b"0.6 0.6 0.6 rg 60 90 80 20 re f\n"


def annot(subtype, rect, **entries):
    return pdf.make_indirect(
        Dictionary(Type=Name.Annot, Subtype=Name("/" + subtype), Rect=Array(rect), F=4, **entries)
    )


annots = Array(
    [
        annot(
            "Line",
            [0, 0, 0, 0],
            L=Array([20, 100, 180, 100]),
            C=Array([1, 0, 0]),
            BS=Dictionary(W=2),
        ),
        annot(
            "PolyLine",
            [170, 170, 190, 190],
            Vertices=Array([20, 20, 60, 40, 100, 20, 140, 40, 180, 20]),
            C=Array([0, 0, 1]),
            BS=Dictionary(W=2),
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
save(pdf, "annotdrawn_2b.pdf", min_version="1.7")
