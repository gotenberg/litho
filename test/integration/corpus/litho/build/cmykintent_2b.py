#!/usr/bin/env python3
"""Build cmykintent_2b.pdf: colours under a rendering intent in a CMYK
page group, PDF/A-2b.

ISO 32000-1, 8.6.5.8 has the ri operator set the intent the colours painted
after it convert under, and 11.3.4 converts each colour painted into a
transparency group into the group's blending colour space. A colour already
in that space keeps its components, which the group's composite reads back
under relative colorimetric, the initial intent: a colour painted under
another intent would then read as relative colorimetric reads it. The
fixture paints in `split`, a version 2.1 CMYK output profile over Lab whose
colorimetric table (A2B1) reads black ink K 0.5 at L* 50 and whose
perceptual table (A2B0) at L* 75; both tables back from Lab invert the
colorimetric one on the black axis.

Two 100 x 200 pages paint the same rows, the first with no page group, the
second with a page group whose /CS is `split`, where a 5 x 5 square of full
black ink painted with Multiply at ca 0.5 in the top right corner makes the
page composite in its group's space. Each row is 80 x 50 from x = 10:

| Row       | Draws                                                          |
| --------- | -------------------------------------------------------------- |
| y 140-190 | `Im`, an 8 x 8 image of K 0.5 in `split`, no ri: L* 50         |
| y 80-130  | `Im` after `/Perceptual ri`: L* 75                             |
| y 20-70   | a fill of `0 0 0 0.5 sc` in `split` after `/Perceptual ri`     |

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, icc_profile, lut8, text_description

pdf = pikepdf.new()


def to_lab(k_dark):
    """A lut8 from CMYK to Lab whose lightness falls by k_dark (encoded L*,
    ICC.1:2001-04, 6.5.8) with full black ink and by 64 with each full
    colour ink, a* and b* 0."""
    clut = []
    for c in (0, 1):
        for m in (0, 1):
            for y in (0, 1):
                for k in (0, 1):
                    clut += [max(0, 255 - k_dark * k - 64 * (c + m + y)), 128, 128]
    return lut8(4, 3, clut)


def from_lab():
    """A lut8 from Lab to CMYK taking lightness to black ink alone."""
    clut = []
    for lightness in (0, 1):
        for _a in (0, 1):
            for _b in (0, 1):
                clut += [0, 0, 0, 0 if lightness else 255]
    return lut8(3, 4, clut)


def split_profile():
    return icc_profile(
        b"prtr",
        b"CMYK",
        b"Lab ",
        [(b"desc", text_description("litho split CMYK intents"))]
        + COMMON_TAGS
        + [(b"A2B0", to_lab(128)), (b"A2B1", to_lab(255)), (b"B2A0", from_lab()), (b"B2A1", from_lab())],
    )


profile = Stream(pdf, split_profile())
profile.N = 4
profile_ref = pdf.make_indirect(profile)

SAMPLES = 8


def page_resources():
    """Resources of one page: its own `split` space and image over the one
    profile stream. PDF/A-4's 6.2.4.2 test 3 judges an indirect ICCBased CMYK
    space once for the document, under the first page that reaches it, as
    veraPDF 1.30.2 does; a space both pages shared would be judged under page
    1, which has no group, and a reordering of the pages would move the
    verdict."""
    split = pdf.make_indirect(Array([Name.ICCBased, profile_ref]))
    image = Stream(pdf, bytes([0, 0, 0, 128]) * SAMPLES * SAMPLES)
    image.Type = Name.XObject
    image.Subtype = Name.Image
    image.Width = SAMPLES
    image.Height = SAMPLES
    image.BitsPerComponent = 8
    image.ColorSpace = split
    return split, Dictionary(
        ColorSpace=Dictionary(S=split),
        XObject=Dictionary(Im=pdf.make_indirect(image)),
        ExtGState=Dictionary(M=Dictionary(Type=Name.ExtGState, BM=Name.Multiply, ca=0.5)),
    )


rows = (
    b"q 80 0 0 50 10 140 cm /Im Do Q "
    b"q /Perceptual ri 80 0 0 50 10 80 cm /Im Do Q "
    b"q /Perceptual ri /S cs 0 0 0 0.5 sc 10 20 80 50 re f Q\n"
)
blend = b"q /M gs /S cs 0 0 0 1 sc 95 195 5 5 re f Q "
# Page 1 paints no transparency, which ISO 19005-2, 6.2.10 would otherwise
# have carry a page group with no output intent.
for content, grouped in ((rows, False), (blend + rows, True)):
    split, resources = page_resources()
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 200]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    if grouped:
        page.Group = Dictionary(Type=Name.Group, S=Name.Transparency, CS=split)
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "cmykintent_2b.pdf")
