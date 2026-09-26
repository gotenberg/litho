#!/usr/bin/env python3
"""Build threedcs_4.pdf: the colour space of a 3D stream, as veraPDF builds it.

A 3D annotation's `/3DD` holds a 3D stream, or a 3D reference dictionary whose
own `/3DD` does (ISO 32000-2, 13.6.3). veraPDF reads the colour space of a
stream whose `/Type` is `/3D` from its `/ColorSpace` alone, never from its
`/Resources` (GFPD3DStream). A name, alone or in a one-element array, reads as
`cs` would select it in the page's resources, so the page's Default colour
space replaces a device family and a `/ColorSpace` entry is read in its place.
Any other array is read with no resources at all: no Default replaces a family
inside it and a name inside it reads as no colour space, DeviceGray to the
`/Indexed` base that holds it. `threed_4e.pdf` plants 3D streams with no colour
space, and no file of any corpus gives one a colour space.

The page's DefaultRGB is an ICCBased space over an abstract profile, which
ISO 19005-4, 6.2.4.2 rejects, so a link it reaches answers there, not at the
DeviceRGB test. Nine 3D annotations sit on the page:

| Annotation | 3D stream `/ColorSpace`                      | Answers       |
| ---------- | -------------------------------------------- | ------------- |
| `A1`       | `/DeviceRGB`, which DefaultRGB replaces      | `6.2.4.2/1`   |
| `A2`       | `[/ICCBased]` over an abstract profile       | `6.2.4.2/1`   |
| `A3`       | `[/Indexed /DeviceRGB ...]`, not replaced    | `6.2.4.3/2`   |
| `A4`       | `/CS0`, a Separation over DeviceCMYK         | `6.2.4.3/3`   |
| `A5`       | `[/Indexed /CS1 ...]`, `/CS1` read as nothing | `6.2.4.3/4`  |
| `A6`       | `/DeviceGray`, through a 3D reference        | `6.2.4.3/4`   |
| `A7`       | none; `/DeviceRGB` in its `/Resources`       | nothing       |
| `A8`       | none; `/DeviceRGB` in a `/3DV` view's stream | nothing       |
| `A9`       | `/DeviceRGB`, on a stream with no `/Type /3D` | nothing      |

The last three are controls, and each carries DeviceRGB, so a checker reading
any of them raises the DeviceRGB count above the one `A3` owes. At PDF/A-4 a 3D
annotation is also a forbidden subtype (ISO 19005-4, 6.3.1), which answers on
its own object; every annotation carries an appearance, so ISO 19005-4, 6.3.3
stays silent.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_4 = 0x04200000
INDEX_TABLE = pikepdf.String(b"\x00\x00\x00\xff\xff\xff")


def abstract_rgb():
    """An abstract profile over RGB, which no PDF/A colour space may name."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_4)
    header[12:16] = b"abst"
    header[16:20] = b"RGB "
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = 3
    return pdf.make_indirect(stream)


def artwork(**keys):
    """A U3D 3D stream, ISO 32000-2, 13.6.3, typed /3D unless keys override it."""
    stream = Stream(pdf, b"litho corpus 3D artwork")
    stream.Type = Name("/3D")
    stream.Subtype = Name("/U3D")
    for key, value in keys.items():
        if value is None:
            del stream[Name("/" + key)]
        else:
            stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


def annotation(index, data, **extra):
    """A 3D annotation with an appearance, so only its stream answers."""
    appearance = Stream(pdf, b"")
    appearance.Type = Name.XObject
    appearance.Subtype = Name.Form
    appearance.BBox = Array([0, 0, 20, 20])
    annot = Dictionary(
        Type=Name.Annot,
        Subtype=Name("/3D"),
        Rect=Array([10 + index * 20, 10, 30 + index * 20, 30]),
        F=4,
        Contents=String("3D artwork A%d" % (index + 1)),
        AP=Dictionary(N=pdf.make_indirect(appearance)),
        **{"3DD": data},
    )
    for key, value in extra.items():
        annot[Name("/" + key)] = value
    return pdf.make_indirect(annot)


tint = Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([1, 0, 0, 0]), N=1)
annotations = [
    annotation(0, artwork(ColorSpace=Name.DeviceRGB)),
    annotation(1, artwork(ColorSpace=Array([Name.ICCBased, abstract_rgb()]))),
    annotation(2, artwork(ColorSpace=Array([Name.Indexed, Name.DeviceRGB, 1, INDEX_TABLE]))),
    annotation(3, artwork(ColorSpace=Name("/CS0"))),
    annotation(4, artwork(ColorSpace=Array([Name.Indexed, Name("/CS1"), 1, INDEX_TABLE]))),
    annotation(5, pdf.make_indirect(Dictionary(Type=Name("/3DRef"), **{"3DD": artwork(ColorSpace=Name.DeviceGray)}))),
    annotation(6, artwork(Resources=Dictionary(ColorSpace=Dictionary(C=Name.DeviceRGB)))),
    annotation(7, artwork(), **{"3DV": Dictionary(**{"3DD": artwork(ColorSpace=Name.DeviceRGB)})}),
    annotation(8, artwork(Type=None, ColorSpace=Name.DeviceRGB)),
]

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(
            ColorSpace=Dictionary(
                DefaultRGB=Array([Name.ICCBased, abstract_rgb()]),
                CS0=Array([Name.Separation, Name("/Spot"), Name.DeviceCMYK, tint]),
                CS1=Name.DeviceRGB,
            )
        ),
        Contents=Stream(pdf, b""),
        Annots=Array(annotations),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "threedcs_4.pdf", min_version="2.0")
