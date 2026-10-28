#!/usr/bin/env python3
"""Build tintwhite_2b.pdf: an RGB ICC profile whose white is tinted, PDF/A-2b.

An ICCBased space reads its colours through its profile (ISO 32000-1,
8.6.5.5). `tw` is a version 2.1 RGB display profile of sRGB's tone curves
and red and green colorants, with its blue colorant at 90 per cent, so its
colorants no longer sum to the PCS white (ICC.1:2001-04, 6.3.4.2) and its
white, every component 1, reads a pale yellow. Little CMS moves a transform's
white onto the destination's when it optimizes it, which poppler and PDFium
never let it do and Ghostscript and MuPDF do; litho draws the profile's white
as the profile reads it and converts it to bare paper.

One 100 x 200 page paints the space three ways:

| y       | x                                                                      |
| ------- | ---------------------------------------------------------------------- |
| 110-190 | 0-100: `Im`, an image of 4 x 4 samples, each row the greys 255, 250    |
|         | and 240 and the colour 128 64 32                                       |
| 60-100  | 10-90: a fill of white (`/T cs 1 1 1 sc`)                              |
| 10-45   | 10-50: a fill of 0.97; 50-90: a fill of 0.5 0.6 0.7                    |

The region the redaction cells clear, the page's centred quarter, x 25-75 and
y 50-150, falls on `Im`'s sample edges and covers its middle two columns and
the middle of the white fill.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, PRIMARIES, icc_profile, text_description, xyz

pdf = pikepdf.new()


def srgb_curve():
    """A curveType (ICC.1:2001-04, 6.5.3) sampling the sRGB transfer
    function (IEC 61966-2-1) at 1024 points."""
    values = []
    for i in range(1024):
        v = i / 1023
        lin = v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4
        values.append(round(lin * 65535))
    return b"curv" + bytes(4) + struct.pack(">I", len(values)) + struct.pack(">%dH" % len(values), *values)


def tinted_profile():
    colorants = dict(PRIMARIES)
    colorants[b"bXYZ"] = tuple(0.9 * v for v in PRIMARIES[b"bXYZ"])
    return icc_profile(
        b"mntr",
        b"RGB ",
        b"XYZ ",
        [(b"desc", text_description("litho tinted white RGB"))]
        + COMMON_TAGS
        + [(sig, xyz(v)) for sig, v in colorants.items()]
        + [(sig, srgb_curve()) for sig in (b"rTRC", b"gTRC", b"bTRC")],
    )


profile = Stream(pdf, tinted_profile())
profile.N = 3
tw = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(profile)]))

ROW = bytes([255, 255, 255, 250, 250, 250, 240, 240, 240, 128, 64, 32])

image = Stream(pdf, ROW * 4)
image.Type = Name.XObject
image.Subtype = Name.Image
image.Width = 4
image.Height = 4
image.BitsPerComponent = 8
image.ColorSpace = tw

resources = Dictionary(
    ColorSpace=Dictionary(T=tw),
    XObject=Dictionary(Im=pdf.make_indirect(image)),
)
content = (
    b"q 100 0 0 80 0 110 cm /Im Do Q "
    b"q /T cs 1 1 1 sc 10 60 80 40 re f Q "
    b"q /T cs 0.97 0.97 0.97 sc 10 10 40 35 re f 0.5 0.6 0.7 sc 50 10 40 35 re f Q\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 200]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "tintwhite_2b.pdf")
