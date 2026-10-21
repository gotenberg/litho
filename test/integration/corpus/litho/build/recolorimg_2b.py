#!/usr/bin/env python3
"""Build recolorimg_2b.pdf: images whose samples a colour conversion has to
read exactly, at PDF/A-2b.

Converting an image to another colour space reads its samples as the filters
leave them, takes each through its /Decode array, and writes them back in the
target. Each image here carries one construct that conversion must keep, and
that no other corpus file reaches:

  Im16   a 4x4 ICCBased RGB image at 16 bits a component, whose precision a
         conversion to 8 bits would drop (ISO 32000-1, 8.9.5.1, Table 89).
  ImLab  a 4x4 CIE L*a*b* image with a non-default /Range, whose components
         are not on [0, 1] (8.6.5.4, Table 65).
  ImKey  a 4x4 ICCBased RGB image with a colour-key /Mask over its raw
         samples (8.9.6.4), which no longer exist once the samples change.
  ImMat  a 4x4 ICCBased RGB image whose soft mask carries a /Matte: its
         samples are preblended with that colour in the image's own space
         (11.6.5.3).
  ImDec  a 4x4 ICCBased RGB image under /Decode [1 0 1 0 1 0], which the
         samples carry only together with it (8.9.5.2).

Every image is in a device-independent space and the page group states its
blending space, so the file needs no output intent and conforms at PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

import zlib

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import rgb_display_profile

pdf = pikepdf.new()

profile = Stream(pdf, zlib.compress(rgb_display_profile()))
profile.N = 3
profile.Filter = Name.FlateDecode
icc_rgb = Array([Name.ICCBased, pdf.make_indirect(profile)])

SIDE = 4


def image(samples, colour_space, bpc=8, **extra):
    stream = Stream(pdf, zlib.compress(bytes(samples)))
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = SIDE
    stream.Height = SIDE
    stream.ColorSpace = colour_space
    stream.BitsPerComponent = bpc
    stream.Filter = Name.FlateDecode
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


def gradient(components, scale=1):
    out = []
    for i in range(SIDE * SIDE):
        for c in range(components):
            v = (i * 37 + c * 71) % 256
            out.append(v)
            if scale == 2:
                out.append((i * 53 + c * 29) % 256)
    return out


im16 = image(gradient(3, scale=2), icc_rgb, bpc=16)
im_lab = image(
    gradient(3),
    Array(
        [
            Name.Lab,
            Dictionary(
                WhitePoint=Array([0.9505, 1, 1.089]),
                Range=Array([-60, 40, -30, 90]),
            ),
        ]
    ),
)
key_samples = gradient(3)
for pixel in (0, 5, 10, 15):
    key_samples[3 * pixel : 3 * pixel + 3] = [3, 4, 5]
im_key = image(key_samples, icc_rgb, Mask=Array([0, 8, 0, 8, 0, 8]))

alpha = [255, 192, 128, 64] * SIDE
matte_mask = Stream(pdf, zlib.compress(bytes(alpha)))
matte_mask.Type = Name.XObject
matte_mask.Subtype = Name.Image
matte_mask.Width = SIDE
matte_mask.Height = SIDE
matte_mask.ColorSpace = Name.DeviceGray
matte_mask.BitsPerComponent = 8
matte_mask.Filter = Name.FlateDecode
matte_mask.Matte = Array([1, 1, 1])
# Preblended with white: c' = m + a(c - m), per ISO 32000-1, 11.6.5.3.
colour = gradient(3)
preblended = []
for i in range(SIDE * SIDE):
    a = alpha[i] / 255
    for c in range(3):
        preblended.append(round(255 + a * (colour[3 * i + c] - 255)))
im_mat = image(preblended, icc_rgb, SMask=pdf.make_indirect(matte_mask))

im_dec = image(gradient(3), icc_rgb, Decode=Array([1, 0, 1, 0, 1, 0]))

names = ["Im16", "ImLab", "ImKey", "ImMat", "ImDec"]
images = [im16, im_lab, im_key, im_mat, im_dec]
body = b"".join(
    b"q 100 0 0 100 %d 600 cm /%s Do Q\n" % (20 + 115 * i, n.encode())
    for i, n in enumerate(names)
)
contents = pdf.make_indirect(Stream(pdf, body))
resources = Dictionary(XObject=Dictionary({"/" + n: im for n, im in zip(names, images)}))
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=contents,
        # The soft mask makes the page transparent, and with no output
        # intent ISO 19005-2, 6.2.10 t2 wants the page group's blending
        # space stated.
        Group=Dictionary(S=Name.Transparency, CS=icc_rgb),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "recolorimg_2b.pdf", min_version="1.7")
