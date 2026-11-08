#!/usr/bin/env python3
"""Build iccalternate_2b.pdf: ICCBased spaces whose profiles no colour engine
can use, each with a CIE-based /Alternate, at PDF/A-2b.

ISO 32000-1, 8.6.5.5, Table 66 has a reader that cannot use an ICCBased
space's profile use the stream's /Alternate, with the colour values as they
are, and the device space of /N components only when /Alternate is absent.
No corpus file reached that: the only unusable profiles carrying an
/Alternate name the device space of /N, which reads the same either way.

Each profile is a version 2.1 header with an empty tag table, as
iccbased_4.pdf's are: veraPDF parses it, and ISO 19005-2, 6.2.4.2 t1 accepts
its version and device class, but it holds no transform, so litho, poppler
(lcms2) and PDF.js (qcms) all fall back to its /Alternate.

| Draws                                              | Space                          |
| -------------------------------------------------- | ------------------------------ |
| a square, `50 20 -30 sc`                           | RGB header, /Alternate Lab     |
| a square, `0.5 sc`                                 | GRAY header, /Alternate CalGray |
| a 2x1 image of two Lab samples                     | the RGB-header space           |
| a 2x1 image of an Indexed space over it            | Indexed, base the RGB-header space |

The RGB-header stream states the Lab ranges in its /Range (Table 66), so the
components the content gives are in range for both spaces.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000
D50 = (0.9642, 1.0, 0.8249)


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def header_only_profile(colour_space):
    """A version 2.1 monitor profile of the colour space with no tags."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + tag_table


LAB_RANGE = [-128, 127, -128, 127]
lab = Array([Name.Lab, Dictionary(WhitePoint=Array(list(D50)), Range=Array(LAB_RANGE))])
cal_gray = Array([Name.CalGray, Dictionary(WhitePoint=Array([0.9505, 1, 1.089]), Gamma=1)])

rgb_profile = Stream(pdf, header_only_profile(b"RGB "))
rgb_profile.N = 3
rgb_profile.Alternate = lab
rgb_profile.Range = Array([0, 100] + LAB_RANGE)
lab_like = Array([Name.ICCBased, pdf.make_indirect(rgb_profile)])

gray_profile = Stream(pdf, header_only_profile(b"GRAY"))
gray_profile.N = 1
gray_profile.Alternate = cal_gray
gray_like = Array([Name.ICCBased, pdf.make_indirect(gray_profile)])


def image(colour_space, samples):
    img = Stream(pdf, bytes(samples))
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 2
    img.Height = 1
    img.ColorSpace = colour_space
    img.BitsPerComponent = 8
    return pdf.make_indirect(img)


# L* 70, a* 60, b* -40 and L* 40, a* -50, b* 50, over the /Range above.
lab_samples = [178, 188, 88, 102, 78, 178]
direct = image(lab_like, lab_samples)
indexed = image(Array([Name.Indexed, lab_like, 1, pikepdf.String(bytes(lab_samples))]), [1, 0])

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 100, 100]),
    Resources=Dictionary(
        ColorSpace=Dictionary(C0=lab_like, C1=gray_like),
        XObject=Dictionary(Im0=direct, Im1=indexed),
    ),
    Contents=Stream(
        pdf,
        b"/C0 cs 50 20 -30 sc 10 55 35 35 re f\n"
        b"/C1 cs 0.5 sc 55 55 35 35 re f\n"
        b"q 35 0 0 35 10 10 cm /Im0 Do Q\n"
        b"q 35 0 0 35 55 10 cm /Im1 Do Q\n",
    ),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "iccalternate_2b.pdf")
