#!/usr/bin/env python3
"""Build jpx_2b.pdf: the five tests ISO 19005-2, 6.2.8.3 states over JPEG 2000.

A coverage scan showed the whole 6.2.8.3 cluster unreached, twenty-five cells
across the five profiles that state it. Nothing blocks it but authorship: every
test reads a box of the JP2 container, not the entropy-coded data, so a fixture
plants each defect by naming a value.

| Image      | Container                            | Answers          |
| ---------- | ------------------------------------ | ---------------- |
| `ImOK`     | 3 channels, one sRGB `colr`          | nothing          |
| `ImChan`   | 2 channels                           | `6.2.8.3/1`      |
| `ImSpecs`  | two `colr` boxes, both APPROX 1      | `6.2.8.3/2`      |
| `ImMeth`   | one `colr` box, METH 4               | `6.2.8.3/3`      |
| `ImJab`    | one `colr` box, enumerated CIEJab    | `6.2.8.3/4`      |
| `ImDepth`  | a `bpcc` box declaring 40 bits       | `6.2.8.3/5`      |
| `ImWaived` | METH 4, and a `/ColorSpace` entry    | nothing          |

The five defects are independent, which is the masking probe: a checker that
reports the first offending image and stops reports one test where five are
due. All seven images are painted.

`ImSpecs` carries two specifications that both set APPROX to 1. Test 2 reads
"there shall be exactly one colour space specification that has the value 0x01
in the APPROX field", so two of them fail it, while test 3 still finds an
effective METH of 1 and stays silent. Two specifications with no APPROX 1 at
all would answer both tests at once and the split would be invisible. That was
measured, on both engines, before the fixture settled on this shape.

`ImWaived` is the exemption planted as a control. ISO 19005-2, 6.2.8.3 hands
the colour tests to the image dictionary's `/ColorSpace` when it has one, so an
image whose `colr` box states a forbidden METH conforms as long as the PDF
names the colour space itself. A check reading the container unconditionally
fires on it.

`ImOK` conforms on all five tests, so a verdict attached to the wrong image
answers on it too.

The document has no `/Metadata` and no output intent, so the metadata and
colour clauses answer from elsewhere in the specification.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _jp2
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

# ISO 19005-2, 6.2.8.3 test 5 permits 1 to 38 bits per component.
FORBIDDEN_DEPTH = 40
FORBIDDEN_METH = 4

IMAGES = (
    ("ImOK", _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, _jp2.ENUM_SRGB)]), False),
    ("ImChan", _jp2.container(_jp2.TWO, 2, 8, [_jp2.colr(1, 1, _jp2.ENUM_SRGB)]), False),
    ("ImSpecs", _jp2.container(_jp2.THREE, 3, 8,
                               [_jp2.colr(1, 1, _jp2.ENUM_SRGB),
                                _jp2.colr(1, 1, _jp2.ENUM_SRGB)]), False),
    ("ImMeth", _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(FORBIDDEN_METH, 1)]), False),
    ("ImJab", _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, _jp2.ENUM_CIEJAB)]), False),
    ("ImDepth", _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, _jp2.ENUM_SRGB)],
                               depths=[FORBIDDEN_DEPTH] * 3), False),
    ("ImWaived", _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(FORBIDDEN_METH, 1)]), True),
)


def image(data, color_space):
    """One JPXDecode image XObject wrapping a JP2 container."""
    stream = Stream(pdf, data)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = _jp2.SIZE
    stream.Height = _jp2.SIZE
    stream.Filter = Name.JPXDecode
    if color_space:
        stream.ColorSpace = Name.DeviceRGB
    return pdf.make_indirect(stream)


xobjects = Dictionary()
lines = []
y = 700
for name, data, color_space in IMAGES:
    xobjects["/" + name] = image(data, color_space)
    lines.append(b"q 40 0 0 40 50 %d cm /%s Do Q" % (y, name.encode()))
    y -= 60

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(XObject=xobjects),
    Contents=Stream(pdf, b"\n".join(lines) + b"\n")))
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "jpx_2b.pdf")
