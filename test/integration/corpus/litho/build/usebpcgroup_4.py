#!/usr/bin/env python3
"""Build usebpcgroup_4.pdf: /UseBlackPtComp /OFF into a CMYK group, PDF/A-4.

ISO 32000-2, Table 57 lets a graphics state turn black point compensation off
(/UseBlackPtComp /OFF) for the colours painted under it. usebpc_4 reaches the
entry in conversions to sRGB alone; no corpus file paints under /OFF into a
CMYK blending colour space, where a colour converts from sRGB into the group's
CMYK profile. This fixture does, on its one 100 x 200 page.

The page paints one form, `G`, over the whole page: an isolated transparency
group whose /CS is `[/ICCBased cmyk]`, the version 4 CMYK output profile of
litho's ICC test data (pkg/internal/graphics/icc/testdata/v4-cmyk-lut.icc),
whose black is the version 4 perceptual reference black, lighter than sRGB's.
A 10 x 5 square painted with Multiply at ca 0.5 in the form's top right corner
makes it blend, so the group is composited in its /CS (an isolated group whose
content blends Normal alone paints straight into its parent). Each row is
eight 10-point steps from x = 10, in `[/ICCBased display]`, the version 2.1
display profile of `_icc.py` (sRGB's primaries, gamma 2.2), whose black is
sRGB's, so only the trip into the group's CMYK depends on the state:

| Row       | Draws                                                        |
| --------- | ------------------------------------------------------------ |
| y 140-190 | fills of `v v v sc`, v = 0, 0.04 ... 0.28, under /ON         |
| y 80-130  | the same fills under /OFF                                    |
| y 20-70   | `Rm`, an 8 x 1 image of the same ramp, samples 0 to 71, /OFF |

Under /ON a colour enters the group compensated and reads back as itself;
under /OFF it enters without compensation, as Little CMS converts without
cmsFLAGS_BLACKPOINTCOMPENSATION, so the shadows below the profile's black
crush to black when the composite is read back.

See ../README.md for the authoritative expected firing set.
"""

import os
import zlib

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import rgb_display_profile

CMYK_PROFILE = os.path.join(
    os.path.dirname(os.path.abspath(__file__)),
    *[".."] * 5,
    "pkg",
    "internal",
    "graphics",
    "icc",
    "testdata",
    "v4-cmyk-lut.icc",
)

pdf = pikepdf.new()

with open(CMYK_PROFILE, "rb") as f:
    cmyk_stream = Stream(pdf, zlib.compress(f.read(), 9))
cmyk_stream.N = 4
cmyk_stream.Filter = Name.FlateDecode
cmyk = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(cmyk_stream)]))

display_stream = Stream(pdf, rgb_display_profile())
display_stream.N = 3
display = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(display_stream)]))

steps = [0.04 * i for i in range(8)]
ramp_image = Stream(pdf, bytes(round(255 * v) for v in steps for _ in range(3)))
ramp_image.Type = Name.XObject
ramp_image.Subtype = Name.Image
ramp_image.Width = len(steps)
ramp_image.Height = 1
ramp_image.BitsPerComponent = 8
ramp_image.ColorSpace = display


def fills(y):
    return b"".join(b"%.2f %.2f %.2f sc %d %d 10 50 re f " % (v, v, v, 10 + 10 * i, y) for i, v in enumerate(steps))


content = (
    b"/S cs "
    b"q /On gs " + fills(140) + b"Q "
    b"q /Off gs " + fills(80) + b"Q "
    b"q /Off gs 80 0 0 50 10 20 cm /Rm Do Q "
    b"q /H gs 0.5 0.5 0.5 sc 90 195 10 5 re f Q\n"
)
form = Stream(pdf, content)
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 100, 200])
form.Group = Dictionary(Type=Name.Group, S=Name.Transparency, I=True, CS=cmyk)
form.Resources = Dictionary(
    ColorSpace=Dictionary(S=display),
    XObject=Dictionary(Rm=pdf.make_indirect(ramp_image)),
    ExtGState=Dictionary(
        Off=Dictionary(Type=Name.ExtGState, UseBlackPtComp=Name.OFF),
        On=Dictionary(Type=Name.ExtGState, UseBlackPtComp=Name.ON),
        H=Dictionary(Type=Name.ExtGState, ca=0.5, BM=Name.Multiply),
    ),
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 200]),
        # ISO 19005-4, 6.2.9: with no output intent, a page that holds
        # transparency names its group's colour space.
        Group=Dictionary(Type=Name.Group, S=Name.Transparency, CS=display),
        Resources=Dictionary(XObject=Dictionary(G=pdf.make_indirect(form))),
        Contents=Stream(pdf, b"/G Do\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=4, rev=2020))
save(pdf, "usebpcgroup_4.pdf", min_version="2.0")
