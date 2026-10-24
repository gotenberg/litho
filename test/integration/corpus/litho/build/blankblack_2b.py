#!/usr/bin/env python3
"""Build blankblack_2b.pdf: images whose zero samples are not black, at
PDF/A-2b.

Redaction blanks the samples an image paints inside a region to the image's
black. Zero on every component is that black in the device and CIE-based
spaces, but ISO 32000-1, 8.6.5.5 lets an ICC profile give a sample any
meaning: in a YCC profile zero is a saturated colour. The one corpus file
holding such an image (pdfjs issue2856, a version 4 YCC-RGB profile) is
outside the render sample the redact-render cell reads, so nothing gated
what a blanked YCC image shows. The other images hold the colour spaces
blanking rewrites in sRGB or whose /Decode it folds into the samples.

| Draws | Space                                                  | Blanking writes                              |
| ----- | ------------------------------------------------------ | -------------------------------------------- |
| Im0   | CalGray with /Decode [1 0]                             | 0, the array folded in and dropped           |
| Im1   | Indexed over CalRGB, entry 0 white                     | sRGB black, in litho's sRGB profile          |
| Im2   | ICCBased, a YCC-like profile (Y, Cb, Cr over Lab), /N 3 | the profile's black, Y 0 and Cb, Cr 128      |
| Im3   | Lab                                                    | sRGB black, in litho's sRGB profile          |
| Im4   | Separation /Spot, CalGray alternate, tint 0 white      | sRGB black, in litho's sRGB profile          |

The YCC-like profile is a version 2.1 input profile whose lut8 A2B0 maps Y
to L* and Cb, Cr to b*, a* over a two-point grid: its black is Y 0, Cb and
Cr 128, and zero samples are L* 0, a* and b* -128, which litho, poppler and
PDFium all show as a saturated blue. Each image is a 16x16 ramp, drawn
side by side, the YCC image in the middle of the page, where every scenario
of the redact-render cell blanks part of it.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, icc_profile, lut8, text_description

pdf = pikepdf.new()

D50 = [0.9642, 1, 0.8249]
SIZE = 16


def ycc_a2b0():
    """Y, Cb, Cr to Lab: L* follows Y, b* follows Cb and a* Cr, each over
    its full range, so the neutral axis is Cb = Cr = 128."""
    clut = []
    for y in (0, 1):
        for cb in (0, 1):
            for cr in (0, 1):
                clut += [255 * y, 255 * cr, 255 * cb]
    return lut8(3, 3, clut)


ycc_profile = icc_profile(
    b"scnr",
    b"RGB ",
    b"Lab ",
    [(b"desc", text_description("litho YCC-like input"))] + COMMON_TAGS + [(b"A2B0", ycc_a2b0())],
)
ycc_stream = Stream(pdf, ycc_profile)
ycc_stream.N = 3
ycc = Array([Name.ICCBased, pdf.make_indirect(ycc_stream)])

cal_gray = Array([Name.CalGray, Dictionary(WhitePoint=Array(D50), Gamma=1)])
cal_rgb = Array([Name.CalRGB, Dictionary(WhitePoint=Array(D50))])
lab = Array([Name.Lab, Dictionary(WhitePoint=Array(D50), Range=Array([-128, 127, -128, 127]))])
spot = Array(
    [
        Name.Separation,
        Name.Spot,
        cal_gray,
        Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([1]), C1=Array([0]), N=1),
    ]
)


def ramp(components):
    """A 16x16 ramp: each sample's components run from light to dark across
    the image, a different phase per component."""
    out = bytearray()
    for y in range(SIZE):
        for x in range(SIZE):
            for c in range(components):
                out.append((x * 16 + y * 4 + c * 64) % 256)
    return bytes(out)


def image(colour_space, samples, **extra):
    img = Stream(pdf, samples)
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = SIZE
    img.Height = SIZE
    img.ColorSpace = colour_space
    img.BitsPerComponent = 8
    for key, value in extra.items():
        img[Name("/" + key)] = value
    return pdf.make_indirect(img)


# Y high, Cb and Cr near neutral: a light picture with a tint.
ycc_samples = bytes(v for i in range(SIZE * SIZE) for v in (160 + i % 64, 96 + i % 32, 150 - i % 32))
# A 16-entry palette, white first, then a ramp of greens.
palette = bytes([255, 255, 255] + [v for i in range(1, 16) for v in (40, 60 + 12 * i, 40)])
indexed_samples = bytes(1 + (x + y) % 15 for y in range(SIZE) for x in range(SIZE))

images = [
    image(cal_gray, ramp(1), Decode=Array([1, 0])),
    image(Array([Name.Indexed, cal_rgb, 15, pikepdf.String(palette)]), indexed_samples),
    image(ycc, ycc_samples),
    image(lab, ramp(3)),
    image(spot, ramp(1)),
]

content = b"".join(b"q 112 0 0 288 %d 43 cm /Im%d Do Q\n" % (16 + 120 * i, i) for i in range(len(images)))
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 392]),
    Resources=Dictionary(XObject=Dictionary({"/Im%d" % i: im for i, im in enumerate(images)})),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "blankblack_2b.pdf")
