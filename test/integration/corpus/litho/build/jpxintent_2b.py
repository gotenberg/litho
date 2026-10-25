#!/usr/bin/env python3
"""Build jpxintent_2b.pdf: a JPEG 2000 image taking its colour from its own
data under the graphics state's rendering intent, PDF/A-2b.

ISO 32000-1, 7.4.9 has a JPXDecode image without /ColorSpace take its colour
from the JPEG 2000 data, and 8.6.5.8 has an image that states no /Intent
converted with the graphics state's, which the ri operator and an ExtGState
/RI set. The image here is one 8 x 8 grey component of samples 128 in a JP2
container whose one colour specification, METH 3 (ISO/IEC 15444-2, M.11.7.2:
any ICC profile; ISO 19005-2, 6.2.8.3 test 3 permits it), carries `rsplit`,
a version 2.1 grey input profile over Lab whose colorimetric table (A2B1)
reads the samples at L* 50 and whose perceptual table (A2B0) at L* 13: the
tables of imageintent_2b's `split`, swapped, so the initial intent draws the
mid grey a reader that ignores the profile draws for sample 128.

One 100 x 200 page paints `Im` three times:

| Where                  | Painted                    | Reads     |
| ---------------------- | -------------------------- | --------- |
| 80 x 50 at (10, 140)   | with no ri                 | mid, 119  |
| 8 x 8 at (10, 80)      | after `/Perceptual ri`     | dark, 33  |
| 8 x 8 at (30, 80)      | after `/P gs`, /RI         | dark, 33  |

The codestream was produced once with OpenJPEG 2.5.4, its COM marker
stripped, as _jp2.py describes:

    python3 -c "open('one.raw','wb').write(bytes([128]*64))"
    opj_compress -i one.raw -o one.j2k -F 8,8,1,8,u -n 1 -r 1

See ../README.md for the authoritative expected firing set.
"""

import base64

import _jp2
from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, icc_profile, lut8, text_description

pdf = pikepdf.new()

ONE = base64.b64decode(
    "/0//UQApAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAABBwEB/1IADAAAAAEA"
    "AAQEAAH/XAAEQED/kAAKAAAAAAAPAAH/k4D/2Q=="
)


def grey_to_lab(white):
    """A lut8 from one grey input to Lab whose lightness rises from 0 to
    white, an encoded L* (ICC.1:2001-04, 6.5.8: v2 Lab encoding, L* * 255 /
    100), with a* and b* 0."""
    return lut8(1, 3, [0, 128, 128, white, 128, 128])


def rsplit_profile():
    return icc_profile(
        b"scnr",
        b"GRAY",
        b"Lab ",
        [(b"desc", text_description("litho reversed split intents"))]
        + COMMON_TAGS
        + [(b"A2B0", grey_to_lab(64)), (b"A2B1", grey_to_lab(255))],
    )


# METH 3, PREC 0, APPROX 1 (ISO/IEC 15444-2, M.11.7.2), then the profile.
colr = _jp2._box(b"colr", bytes([3, 0, 1]) + rsplit_profile())
image = Stream(pdf, _jp2.container(ONE, 1, 8, [colr]))
image.Type = Name.XObject
image.Subtype = Name.Image
image.Width = _jp2.SIZE
image.Height = _jp2.SIZE
image.Filter = Name.JPXDecode

resources = Dictionary(
    XObject=Dictionary(Im=pdf.make_indirect(image)),
    ExtGState=Dictionary(P=Dictionary(Type=Name.ExtGState, RI=Name.Perceptual)),
)
content = (
    b"q 80 0 0 50 10 140 cm /Im Do Q "
    b"q /Perceptual ri 8 0 0 8 10 80 cm /Im Do Q "
    b"q /P gs 8 0 0 8 30 80 cm /Im Do Q\n"
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
save(pdf, "jpxintent_2b.pdf")
