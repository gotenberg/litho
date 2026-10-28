#!/usr/bin/env python3
"""Build imageintent_2b.pdf: images under the graphics state's rendering
intent, PDF/A-2b.

ISO 32000-1, 8.6.5.8 has an image's /Intent override the current rendering
intent, which the ri operator and an ExtGState /RI set: an image that states
no /Intent is converted with the graphics state's. Every image here is an
ICCBased grey image of 8 x 8 samples, all 128, under profile `split`, a
version 2.1 grey input profile over Lab whose perceptual table (A2B0) reads
its samples as greys of their own lightness and whose colorimetric table
(A2B1) reads them at a quarter of that lightness. Both tables stay on the
neutral axis, which is all litho reads a grey profile on. Perceptual and
saturation (no A2B2: readers fall back on A2B0) draw sample 128 as a mid
grey, L* 50; relative and absolute colorimetric, and the initial intent, draw
it dark, L* 13. Black is L* 0 under both tables, so black point compensation
moves neither.

Two 100 x 200 pages, each painting three 80 x 50 images from x = 10:

| Page | y 140-190                | y 80-130                                | y 20-70                                |
| ---- | ------------------------ | --------------------------------------- | -------------------------------------- |
| 1    | `Im`, no ri              | `Im` after `/Perceptual ri`             | `Im` after `/AbsoluteColorimetric ri`  |
| 2    | `Im` after `/P gs` (/RI) | `Own`, /Intent /RelativeColorimetric,   | an inline image after `/Perceptual ri` |
|      |                          | after `/Perceptual ri`                  |                                        |

so page 1 reads dark, mid, dark, and page 2 mid, dark, mid.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, icc_profile, lut8, text_description

pdf = pikepdf.new()


def grey_to_lab(white):
    """A lut8 from one grey input to Lab whose lightness rises from 0 to
    white, an encoded L* (ICC.1:2001-04, 6.5.8: v2 Lab encoding, L* * 255 /
    100), with a* and b* 0."""
    return lut8(1, 3, [0, 128, 128, white, 128, 128])


def split_profile():
    return icc_profile(
        b"scnr",
        b"GRAY",
        b"Lab ",
        [(b"desc", text_description("litho split intents"))]
        + COMMON_TAGS
        + [(b"A2B0", grey_to_lab(255)), (b"A2B1", grey_to_lab(64))],
    )


profile = Stream(pdf, split_profile())
profile.N = 1
split = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(profile)]))


# Eight samples a side, so a region over part of an image covers whole samples.
SAMPLES = 8


def image(**extra):
    stream = Stream(pdf, bytes([128]) * SAMPLES * SAMPLES)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = SAMPLES
    stream.Height = SAMPLES
    stream.BitsPerComponent = 8
    stream.ColorSpace = split
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


resources = Dictionary(
    ColorSpace=Dictionary(S=split),
    XObject=Dictionary(Im=image(), Own=image(Intent=Name.RelativeColorimetric)),
    ExtGState=Dictionary(P=Dictionary(Type=Name.ExtGState, RI=Name.Perceptual)),
)
inline = b"BI /W 8 /H 8 /BPC 8 /CS /S ID " + b"\x80" * SAMPLES * SAMPLES + b" EI"
for content in [
    b"q 80 0 0 50 10 140 cm /Im Do Q "
    b"q /Perceptual ri 80 0 0 50 10 80 cm /Im Do Q "
    b"q /AbsoluteColorimetric ri 80 0 0 50 10 20 cm /Im Do Q\n",
    b"q /P gs 80 0 0 50 10 140 cm /Im Do Q "
    b"q /Perceptual ri 80 0 0 50 10 80 cm /Own Do Q "
    b"q /Perceptual ri 80 0 0 50 10 20 cm " + inline + b" Q\n",
]:
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
save(pdf, "imageintent_2b.pdf")
