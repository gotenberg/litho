#!/usr/bin/env python3
"""Build greytint_2b.pdf: a grey ICC profile that tints its greys, PDF/A-2b.

An ICCBased space reads its colours through its profile (ISO 32000-1,
8.6.5.5), and a grey profile's AToB table maps a grey sample to any PCS
colour (ICC.1:2001-04, 6.5.8), not only to the neutral axis a grey tone curve
stays on. `tint` is a version 2.1 grey input profile over Lab whose lut8
AToB0 table, on a three-point grid, reads sample 0 as black (L* 0), sample
255 as white (L* 100), and the middle sample as a warm brown, L* 50, a* 30,
b* 20, the samples between interpolated. Black and white stay neutral, so
neither black point compensation nor Little CMS's white fix-up moves them.

One 100 x 200 page paints the space four ways:

| y       | x                                                                       |
| ------- | ----------------------------------------------------------------------- |
| 110-190 | 0-100: `Im`, an image of 4 x 4 samples, each row 0, 85, 170 and 255     |
| 60-100  | 10-90: a fill of colour 0.5 (`/S cs 0.5 sc`)                            |
| 10-45   | 10-50: an axial shading from 0 to 1; 50-90: an inline image of 4 x 1    |
|         | samples 0, 85, 170 and 255                                              |

so every middle grey reads brown, as poppler draws it. The shading and the
inline image lie below the page's centred quarter, x 25-75 and y 50-150, the
region the redaction cells clear, since a redaction refuses to cross a
shading. The region's edges fall between `Im`'s samples, so it covers the
middle two samples of the bottom two rows whole.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, IDENTITY, icc_profile, s15, text_description

pdf = pikepdf.new()


def grey_to_tinted_lab():
    """A lut8 (ICC.1:2001-04, 6.5.8) from one grey input to Lab over a
    three-point grid: L* 0, then L* 50 a* 30 b* 20, then L* 100, in the v2
    8-bit Lab encoding (L* * 255 / 100, a* and b* + 128)."""
    ramp = bytes(range(256))
    clut = bytes([0, 128, 128, round(50 * 255 / 100), 128 + 30, 128 + 20, 255, 128, 128])
    return b"mft1" + bytes(4) + bytes([1, 3, 3, 0]) + s15(*IDENTITY) + ramp + clut + ramp * 3


def tint_profile():
    return icc_profile(
        b"scnr",
        b"GRAY",
        b"Lab ",
        [(b"desc", text_description("litho tinted grey"))] + COMMON_TAGS + [(b"A2B0", grey_to_tinted_lab())],
    )


profile = Stream(pdf, tint_profile())
profile.N = 1
tint = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(profile)]))

RAMP = bytes([0, 85, 170, 255])

image = Stream(pdf, RAMP * 4)
image.Type = Name.XObject
image.Subtype = Name.Image
image.Width = len(RAMP)
image.Height = 4
image.BitsPerComponent = 8
image.ColorSpace = tint

shading = Dictionary(
    ShadingType=2,
    ColorSpace=tint,
    Coords=Array([10, 0, 50, 0]),
    Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0]), C1=Array([1]), N=1),
)

resources = Dictionary(
    ColorSpace=Dictionary(S=tint),
    XObject=Dictionary(Im=pdf.make_indirect(image)),
    Shading=Dictionary(Sh=pdf.make_indirect(shading)),
)
inline = b"BI /W 4 /H 1 /BPC 8 /CS /S ID " + RAMP + b" EI"
content = (
    b"q 100 0 0 80 0 110 cm /Im Do Q "
    b"q /S cs 0.5 sc 10 60 80 40 re f Q "
    b"q 10 10 40 35 re W n /Sh sh Q "
    b"q 40 0 0 35 50 10 cm " + inline + b" Q\n"
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
save(pdf, "greytint_2b.pdf")
