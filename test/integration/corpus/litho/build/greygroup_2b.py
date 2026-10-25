#!/usr/bin/env python3
"""Build greygroup_2b.pdf: colours entering grey ICCBased transparency
groups, PDF/A-2b.

ISO 32000-1, 11.3.4 converts a colour painted into a transparency group into
the group's blending colour space, which for an ICCBased space is its
profile's (8.6.5.5). A grey profile whose tone curve is not sRGB's makes the
conversion visible: DeviceGray's formula of 10.3 (0.3 red, 0.59 green, 0.11
blue) gives a grey that the profile then reads as another colour. Every page
names a space of its own:

| Page | Group /CS                     | Draws                                                    |
| ---- | ----------------------------- | -------------------------------------------------------- |
| 1    | `linear`: XYZ, tone curve 1.0 | an RGB grey 0.66 over the page                           |
| 2    | RGB (page), `linear` (mask)   | black through a luminosity soft mask of an RGB grey 0.5  |
| 3    | `lstar`: Lab, AToB0/1, BToA0/1 | a fill of the group's own space at 0.5 under /Perceptual |

Pages 1 and 3 also paint a 2 x 2 point square at ca 0.5 in their bottom left
corner, the transparency that makes the page group blend in its space.

`linear` is a version 2.1 grey display profile over XYZ with a tone curve of
gamma 1.0, so a grey component is the luminance Y. `lstar` is a version 2.1
grey display profile over Lab whose relative colorimetric tables (AToB1,
BToA1) give L* = 100 x grey, while its perceptual ones (AToB0, BToA0) read
0.5 as L* 70: the perceptual colour cannot keep its components in the
group, read back under relative colorimetric, and reaches it through sRGB.
RGB colours are in `rgb`, a gamma 2.2 display profile with the sRGB
primaries.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, IDENTITY, gamma_curve, icc_profile, rgb_display_profile, s15, text_description

pdf = pikepdf.new()


def icc_space(data, n):
    stream = Stream(pdf, data)
    stream.N = n
    return pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(stream)]))


def linear_profile():
    return icc_profile(
        b"mntr",
        b"GRAY",
        b"XYZ ",
        [(b"desc", text_description("litho linear grey"))] + COMMON_TAGS + [(b"kTRC", gamma_curve(1.0))],
    )


def grey_to_lab(lightness):
    """A lut8 (ICC.1:2001-04, 6.5.8) from one grey input to neutral Lab over
    a grid of the given L* values, in the v2 8-bit encoding (L* * 255 / 100)."""
    ramp = bytes(range(256))
    clut = b"".join(bytes([round(v * 255 / 100), 128, 128]) for v in lightness)
    return b"mft1" + bytes(4) + bytes([1, 3, len(lightness), 0]) + s15(*IDENTITY) + ramp + clut + ramp * 3


LAB_GRID = 11


def lab_to_grey(grey):
    """A lut8 from Lab to one grey output over an 11-point grid, L* 0, 10 ...
    100 (a* and b* ignored), the grey of each L* given by grey."""
    ramp = bytes(range(256))
    clut = bytes(round(255 * grey(10 * i)) for i in range(LAB_GRID) for _ in range(LAB_GRID * LAB_GRID))
    return b"mft1" + bytes(4) + bytes([3, 1, LAB_GRID, 0]) + s15(*IDENTITY) + ramp * 3 + clut + ramp


def perceptual_grey(lightness):
    """The inverse of AToB0's L* 0, 70, 100 at grey 0, 0.5, 1."""
    if lightness <= 70:
        return 0.5 * lightness / 70
    return 0.5 + 0.5 * (lightness - 70) / 30


def lstar_profile():
    return icc_profile(
        b"mntr",
        b"GRAY",
        b"Lab ",
        [(b"desc", text_description("litho L* grey"))]
        + COMMON_TAGS
        + [
            (b"kTRC", gamma_curve(1.0)),
            (b"A2B0", grey_to_lab([0, 70, 100])),
            (b"A2B1", grey_to_lab([0, 100])),
            (b"B2A0", lab_to_grey(perceptual_grey)),
            (b"B2A1", lab_to_grey(lambda lightness: lightness / 100)),
        ],
    )


def transparency_group(cs):
    return Dictionary(Type=Name.Group, S=Name.Transparency, CS=cs)


def add_page(content, resources, group):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 100]),
        Resources=resources,
        Contents=Stream(pdf, content),
        Group=group,
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


half = Dictionary(Type=Name.ExtGState, ca=0.5)

rgb1 = icc_space(rgb_display_profile(), 3)
linear1 = icc_space(linear_profile(), 1)
add_page(
    b"/RGB cs 0.66 0.66 0.66 sc 0 0 100 100 re f q /A gs 0.2 0.2 0.2 sc 0 0 2 2 re f Q\n",
    Dictionary(ColorSpace=Dictionary(RGB=rgb1), ExtGState=Dictionary(A=half)),
    transparency_group(linear1),
)

rgb2 = icc_space(rgb_display_profile(), 3)
linear2 = icc_space(linear_profile(), 1)
mask_group = Stream(pdf, b"/RGB cs 0.5 0.5 0.5 sc 0 0 100 100 re f\n")
mask_group.Type = Name.XObject
mask_group.Subtype = Name.Form
mask_group.BBox = Array([0, 0, 100, 100])
mask_group.Group = transparency_group(linear2)
mask_group.Resources = Dictionary(ColorSpace=Dictionary(RGB=rgb2))
masked = Dictionary(
    Type=Name.ExtGState,
    SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=pdf.make_indirect(mask_group)),
)
add_page(
    b"/RGB cs 1 1 1 sc 0 0 100 100 re f q /M gs 0 0 0 sc 10 10 80 80 re f Q\n",
    Dictionary(ColorSpace=Dictionary(RGB=rgb2), ExtGState=Dictionary(M=masked)),
    transparency_group(rgb2),
)

lstar3 = icc_space(lstar_profile(), 1)
add_page(
    b"q /Perceptual ri /L cs 0.5 sc 0 50 100 50 re f Q q /A gs /L cs 0 sc 0 0 2 2 re f Q\n",
    Dictionary(ColorSpace=Dictionary(L=lstar3), ExtGState=Dictionary(A=half)),
    transparency_group(lstar3),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "greygroup_2b.pdf")
