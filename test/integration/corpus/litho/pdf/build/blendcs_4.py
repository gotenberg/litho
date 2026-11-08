#!/usr/bin/env python3
"""Build blendcs_4.pdf: which transparency blending spaces cover device colour.

ISO 19005-4, 6.2.4.3 lets DeviceRGB and DeviceCMYK stand when the current
transparency blending space is a device-independent space of the same model,
where ISO 19005-2 asks for an output intent alone. veraPDF tests the blending
space's type: an ICC profile whose header names `RGB `, or a CalRGB space, for
DeviceRGB (t2), an ICC profile naming `CMYK` for DeviceCMYK (t3). Its DeviceGray
test (t4) reads the output intent and nothing else, so no blending space covers
DeviceGray. It reads a group's `/CS` with no resources, so a `/CS` naming a
`/ColorSpace` entry is no blending space at all. No file of any corpus paints a
device colour inside an isolated group over a gray, CalGray or Lab space, or
under a group whose `/CS` is a name.

The first page's own group names `/CS0`, an ICCBased space over an RGB profile
the page's resources define. The second page paints six forms, each with an
isolated group:

| Stream      | Group `/CS`                          | Paints    | 6.2.4.3 |
| ----------- | ------------------------------------ | --------- | ------- |
| page 1      | `/CS0`, a name                       | `rg`, `f` | `/2` x2 |
| `FmName`    | `/CS1`, a name of its own entry      | `rg`, `f` | `/2` x2 |
| `FmGray`    | `[/ICCBased]` over a `GRAY` profile  | `g`, `f`  | `/4` x2 |
| `FmCalGray` | `[/CalGray]`                         | `g`, `f`  | `/4` x2 |
| `FmLab`     | `[/Lab]`                             | `g`, `f`  | `/4` x2 |
| `FmRGB`     | `[/ICCBased]` over an `RGB ` profile | `rg`, `f` | silent  |
| `FmCalRGB`  | `[/CalRGB]`                          | `rg`, `f` | silent  |

The last two are the controls: each covers the DeviceRGB its form paints, so a
checker that reads no blending space at all reports them. They are painted
last, after every form whose group covers nothing.

The groups sit on two pages because ISO 19005-4, 6.2.9 t2 asks a page that uses
transparency for a blending space, which a page's own group does not make it
use: the second page's forms use transparency and it has no group, which both
tools fail once, and the first page paints nothing transparent, so its named
group owes the rule nothing.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_4 = 0x04200000
FILL = b"0 0 10 10 re f\n"
WHITE = Dictionary(WhitePoint=Array([0.9505, 1, 1.089]))


def icc_profile(colour_space, components):
    """A display profile whose header names colour_space, and no tags."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_4)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = components
    return pdf.make_indirect(stream)


rgb_profile = icc_profile(b"RGB ", 3)
gray_profile = icc_profile(b"GRAY", 1)


def group(colour_space, isolated):
    """A transparency group dictionary blending in colour_space."""
    g = Dictionary(Type=Name.Group, S=Name.Transparency, CS=colour_space)
    if isolated:
        g.I = True
    return g


def form(colour_space, paint, resources=None):
    """A form XObject with an isolated transparency group."""
    stream = Stream(pdf, paint + FILL)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 10, 10])
    stream.Resources = resources if resources is not None else Dictionary()
    stream.Group = group(colour_space, True)
    return pdf.make_indirect(stream)


RG = b"1 0 0 rg "
G = b"0.5 g "
forms = [
    ("FmName", form(Name("/CS1"), RG, Dictionary(ColorSpace=Dictionary(CS1=Array([Name.ICCBased, rgb_profile]))))),
    ("FmGray", form(Array([Name.ICCBased, gray_profile]), G)),
    ("FmCalGray", form(Array([Name.CalGray, WHITE]), G)),
    ("FmLab", form(Array([Name.Lab, WHITE]), G)),
    ("FmRGB", form(Array([Name.ICCBased, rgb_profile]), RG)),
    ("FmCalRGB", form(Array([Name.CalRGB, WHITE]), RG)),
]

grouped = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(ColorSpace=Dictionary(CS0=Array([Name.ICCBased, rgb_profile]))),
        Group=group(Name("/CS0"), False),
        Contents=Stream(pdf, b"q " + RG + FILL + b"Q\n"),
    )
)
painting = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(XObject=Dictionary({"/" + name: obj for name, obj in forms})),
        Contents=Stream(pdf, b"".join(b"q /%s Do Q\n" % name.encode() for name, _ in forms)),
    )
)
pdf.pages.append(pikepdf.Page(grouped))
pdf.pages.append(pikepdf.Page(painting))

# Deliberately no /Metadata and no output intent.
save(pdf, "blendcs_4.pdf", min_version="2.0")
