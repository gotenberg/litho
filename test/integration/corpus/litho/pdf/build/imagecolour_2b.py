#!/usr/bin/env python3
"""Build imagecolour_2b.pdf: images whose colour no other corpus file states.

No corpus file holds an image over the colorant None, nor a JPXDecode image
whose data names CIELab, ROMM-RGB or e-sRGB. The litho corpus runs the Writer
mutations, so the redaction of page 1 decodes every image here.

| Image      | Colour                                             | Reading            |
| ---------- | -------------------------------------------------- | ------------------ |
| `SepNone`  | `[/Separation /None /DeviceGray f]`                | paints nothing     |
| `DevNone`  | `[/DeviceN [/None /None] /DeviceGray f2]`          | paints nothing     |
| `JLabDef`  | JPX, enumerated CIELab, no EP fields               | T.42 default Lab   |
| `JLabEP`   | JPX, enumerated CIELab, EP fields, illuminant D50  | stated Lab         |
| `JROMM`    | JPX, enumerated ROMM-RGB                           | ISO 22028-2        |
| `JESRGB`   | JPX, enumerated e-sRGB                             | DeviceRGB fallback |

ISO 32000-1, 8.6.6.4 says the colorant None never produces visible output,
and 8.6.6.5 says the same of a DeviceN space naming only None. The JPX images
carry no /ColorSpace, so ISO 32000-1, 7.4.9 takes their colour from the data,
and ISO 19005-2, 6.2.8.3 test 4 forbids only CIEJab among the enumerated
spaces.

The document has no /Metadata and no output intent, so the metadata and
colour clauses answer from elsewhere in the specification.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _jp2
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

# ISO/IEC 15444-2, Table M.25.
ENUM_CIELAB = 14
ENUM_ESRGB = 20
ENUM_ROMM = 21
# ISO/IEC 15444-2, M.11.7.4.1: IL of CIE illuminant D50.
IL_D50 = 0x00443530


def lab_colr(ep=None):
    """An enumerated CIELab colour specification, with its EP fields if any."""
    payload = bytes([1, 0, 1]) + struct.pack(">I", ENUM_CIELAB)
    if ep is not None:
        payload += struct.pack(">7I", *ep)
    return _jp2._box(b"colr", payload)


def tinted(samples, color_space):
    """An 8x1 image in a Separation or DeviceN space."""
    stream = Stream(pdf, samples)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 8
    stream.Height = 1
    stream.ColorSpace = color_space
    stream.BitsPerComponent = 8
    return pdf.make_indirect(stream)


def jpx(data):
    """One JPXDecode image XObject wrapping a JP2 container, no /ColorSpace."""
    stream = Stream(pdf, data)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = _jp2.SIZE
    stream.Height = _jp2.SIZE
    stream.Filter = Name.JPXDecode
    return pdf.make_indirect(stream)


# Tint 0 paints paper white and full tint black, as a real ink would.
tint = pdf.make_indirect(Dictionary(FunctionType=2, Domain=Array([0, 1]), Range=Array([0, 1]),
                                    C0=Array([1]), C1=Array([0]), N=1))
tint2 = Stream(pdf, bytes([255, 128, 128, 0]))
tint2.FunctionType = 0
tint2.Domain = Array([0, 1, 0, 1])
tint2.Range = Array([0, 1])
tint2.Size = Array([2, 2])
tint2.BitsPerSample = 8
tint2 = pdf.make_indirect(tint2)

ramp = bytes(range(0, 256, 32))
IMAGES = (
    ("SepNone", tinted(ramp, Array([Name.Separation, Name("/None"), Name.DeviceGray, tint]))),
    ("DevNone", tinted(bytes(b for v in ramp for b in (v, 255 - v)),
                       Array([Name.DeviceN, Array([Name("/None"), Name("/None")]), Name.DeviceGray, tint2]))),
    ("JLabDef", jpx(_jp2.container(_jp2.THREE, 3, 8, [lab_colr()]))),
    ("JLabEP", jpx(_jp2.container(_jp2.THREE, 3, 8, [lab_colr((100, 0, 255, 128, 255, 128, IL_D50))]))),
    ("JROMM", jpx(_jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, ENUM_ROMM)]))),
    ("JESRGB", jpx(_jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, ENUM_ESRGB)]))),
)

xobjects = Dictionary()
lines = []
y = 700
for name, ref in IMAGES:
    xobjects["/" + name] = ref
    lines.append(b"q 80 0 0 40 50 %d cm /%s Do Q" % (y, name.encode()))
    y -= 60

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(XObject=xobjects),
    Contents=Stream(pdf, b"\n".join(lines) + b"\n")))
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "imagecolour_2b.pdf")
