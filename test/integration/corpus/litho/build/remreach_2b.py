#!/usr/bin/env python3
"""Build remreach_2b.pdf: constructs compliance judges below a page's own
resources, where the repairs of their rules did not reach.

Six DeviceN colour spaces over one spot colorant each carry no /Colorants,
which ISO 19005-2, 6.2.4.4 t1 fails wherever a colour link reaches the space:

| Spot    | Where the DeviceN stands                                     |
| ------- | ------------------------------------------------------------ |
| `SpotA` | the `/ColorSpace` resources of a form the page draws         |
| `SpotB` | the colour space of an image the page draws                  |
| `SpotC` | the resources of a coloured tiling pattern the page fills    |
| `SpotD` | the base of an `/Indexed` space the page selects             |
| `SpotE` | the resources of the Type 3 font whose glyph the page shows  |
| `SpotF` | the resources of an annotation's normal appearance           |

The pattern's cell also draws an image with /Interpolate true (6.2.8 t3). The
catalog's /Names /EmbeddedFiles tree names a file specification typed /F, which
ISO 32000-1, 7.11.4 makes a file specification by position, and no /AF lists
it (ISO 19005-3, 6.8 t4).

The /Colorants repair walked the page's /ColorSpace entries and shadings, the
XObject repair the page's /XObject resources and the forms they name, and the
association repair the file specifications typed /Filespec, so each of these
kept its finding through remediation. A CMYK OutputIntent covers the DeviceCMYK
alternates.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as iccbased_4.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", b"OUTI"))
destination.N = 4
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)

tint = pdf.make_indirect(
    Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([0, 0.5, 1, 0]), N=1)
)


def devicen(spot):
    """A DeviceN over one spot colorant with no /Colorants."""
    return Array([Name.DeviceN, Array([Name("/" + spot)]), Name.DeviceCMYK, tint])


FILL = b"/CS cs 1 scn 0 0 5 5 re f"


def resources(spot, **extra):
    d = Dictionary(ColorSpace=Dictionary(CS=devicen(spot)))
    for key, value in extra.items():
        d[Name("/" + key)] = value
    return d


def form(body, res, **extra):
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 20, 20])
    stream.Resources = res
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


def image(sample, colour_space, **extra):
    stream = Stream(pdf, sample)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 1
    stream.Height = 1
    stream.ColorSpace = colour_space
    stream.BitsPerComponent = 8
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


fm = form(FILL, resources("SpotA"))
im = image(b"\x80", devicen("SpotB"))

interpolated = image(b"\x40", Name.DeviceGray, Interpolate=True)
cell = Stream(pdf, FILL + b" q 5 0 0 5 5 5 cm /Im Do Q")
cell.Type = Name.Pattern
cell.PatternType = 1
cell.PaintType = 1
cell.TilingType = 1
cell.BBox = Array([0, 0, 10, 10])
cell.XStep = 10
cell.YStep = 10
cell.Resources = resources("SpotC", XObject=Dictionary(Im=interpolated))
pattern = pdf.make_indirect(cell)

glyph = pdf.make_indirect(Stream(pdf, b"1000 0 d0 /CS cs 1 scn 0 0 1000 1000 re f"))
t3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=glyph),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a])),
        FirstChar=97,
        LastChar=97,
        Widths=Array([1000]),
        Resources=resources("SpotE"),
    )
)

appearance = form(FILL, resources("SpotF"))
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([100, 100, 120, 120]),
        F=4,
        AP=Dictionary(N=appearance),
    )
)

content = (
    b"/Fm0 Do\n"
    b"q 10 0 0 10 20 20 cm /Im0 Do Q\n"
    b"q /Pattern cs /P0 scn 30 0 50 50 re f Q\n"
    b"q /CSI cs 1 sc 90 0 10 10 re f Q\n"
    b"BT /T3 12 Tf 10 150 Td (a) Tj ET\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(
            XObject=Dictionary(Fm0=fm, Im0=im),
            Pattern=Dictionary(P0=pattern),
            Font=Dictionary(T3=t3),
            ColorSpace=Dictionary(CSI=Array([Name.Indexed, devicen("SpotD"), 1, String(b"\x00\xff")])),
        ),
        Contents=pdf.make_indirect(Stream(pdf, content)),
        Annots=Array([annot]),
    )
)
pdf.pages.append(pikepdf.Page(page))

embedded = Stream(pdf, b"litho\n")
embedded.Type = Name.EmbeddedFile
embedded.Subtype = Name("/text/plain")
spec = pdf.make_indirect(
    Dictionary(
        Type=Name.F,
        F=String("x.txt"),
        UF=String("x.txt"),
        AFRelationship=Name.Unspecified,
        EF=Dictionary(F=pdf.make_indirect(embedded)),
    )
)
pdf.Root.Names = Dictionary(EmbeddedFiles=Dictionary(Names=Array([String("x.txt"), spec])))
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))

save(pdf, "remreach_2b.pdf", min_version="1.7")
