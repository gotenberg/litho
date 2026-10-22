#!/usr/bin/env python3
"""Build flattentiles_1b.pdf: transparency over a photograph and a drawing
side by side, at PDF/A-1b.

A PDF/A-1 remediation flattens the transparency ISO 19005-1, 6.4 forbids and
encodes a composite of images and shadings only as JPEG. A composite that
mixes a photograph with vector art is cut into tiles: those only the
photograph reaches are JPEG, the others stay lossless. The corpus files that
reach this are government reports of tens of megabytes. This page holds one
such region:

| Draws                                                                     |
| ------------------------------------------------------------------------- |
| a 160 x 120 photograph, smooth colour with grain, over 192 x 144 pt       |
| a stroked chart beside it: a frame, a polyline and filled triangle marks  |
| a red band at ca 0.5 across both, which joins them into one region        |

At the default 300 dpi the band's composite is cut into 128-pixel cells: the
cells over the photograph are JPEG, those the chart's frame, line and marks
reach stay lossless, and so does the short last row of cells along the band's
bottom, too few pixels to hold 256 colours.

The file declares no OutputIntent, so the DeviceRGB paint fails 6.2.3.3 t1 and
the DeviceGray paint 6.2.3.3 t3 at PDF/A-1b, and the constant alpha fails 6.4
t6.

See ../README.md for the authoritative expected firing set.
"""

import zlib

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

W, H = 160, 120


def photo_samples():
    out = bytearray()
    seed = 12345
    for y in range(H):
        for x in range(W):
            seed = (seed * 1103515245 + 12345) & 0x7FFFFFFF
            grain = (seed >> 16) % 7 - 3
            r = 40 + x * 160 // W + grain
            g = 60 + y * 140 // H + grain
            b = 150 - (x + y) * 90 // (W + H) + grain
            out += bytes(max(0, min(255, v)) for v in (r, g, b))
    return bytes(out)


photo = Stream(pdf, zlib.compress(photo_samples(), 9))
photo.Type = Name.XObject
photo.Subtype = Name.Image
photo.Width = W
photo.Height = H
photo.ColorSpace = Name.DeviceRGB
photo.BitsPerComponent = 8
photo.Filter = Name.FlateDecode
photo = pdf.make_indirect(photo)

half = pdf.make_indirect(Dictionary(Type=Name.ExtGState, ca=0.5))

CONTENT = (
    # The photograph, 2.67 x 2 inches at the page's left.
    b"q 192 0 0 144 48 480 cm /Photo Do Q\n"
    # The chart at its right: a frame, a polyline and triangle marks.
    b"0 g 1 w 260 480 204 144 re S\n"
    b"0 0 0.6 RG 2 w 272 490 m 310 540 l 350 520 l 390 600 l 452 570 l S\n"
    b"0.8 0 0 rg 305 535 m 315 535 l 310 545 l h f 385 595 m 395 595 l 390 605 l h f\n"
    # A red band at ca 0.5 across the photograph and the chart.
    b"q /Half gs 1 0 0 rg 48 540 416 40 re f Q\n"
)

pdf.pages.append(
    pikepdf.Page(
        pdf.make_indirect(
            Dictionary(
                Type=Name.Page,
                MediaBox=Array([0, 0, 612, 792]),
                Resources=Dictionary(
                    XObject=Dictionary(Photo=photo),
                    ExtGState=Dictionary(Half=half),
                ),
                Contents=Stream(pdf, CONTENT),
            )
        )
    )
)

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))

save(pdf, "flattentiles_1b.pdf", min_version="1.4")
