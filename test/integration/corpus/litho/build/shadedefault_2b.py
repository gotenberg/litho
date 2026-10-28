#!/usr/bin/env python3
"""Build shadedefault_2b.pdf: shading patterns over DeviceCMYK beside DeviceRGB.

A Default colour space replaces a device family the content stream selects
through its own resources (ISO 32000-1, 8.6.5.6), and a shading pattern has no
resources of its own (8.7.4.1, Table 76). veraPDF 1.30.2 therefore judges the
device family of a shading pattern's shading against the output intent alone.
The sole intent here is a CMYK one, which covers every DeviceCMYK, and page 1
paints DeviceRGB, which it does not. Remediation covers DeviceRGB by replacing
the intent with an sRGB one and gives DeviceCMYK a /DefaultCMYK, which reaches
none of these shadings:

| Page | Path                                     | Shading colour space                                |
| ---- | ---------------------------------------- | --------------------------------------------------- |
| 1    | `rg` fill, `/P1 scn`                     | Separation `/Spot` over DeviceCMYK, both indirect   |
| 2    | `/P2 scn`, pattern and shading direct    | DeviceCMYK                                          |
| 3    | `/P3 scn`                                | DeviceN `/Spot2` over DeviceCMYK, `/Colorants` too  |
| 4    | `/CS0 cs`, `k` fill, `/P1 scn`           | the page's `/CS0` is page 1's Separation `/Spot`    |
| 5    | a form whose own resources hold `/P5`    | Separation `/Spot` over DeviceCMYK                  |

Page 4 selects the colorant of page 1's shading through a /ColorSpace entry,
which a /DefaultCMYK reaches, so the remediated output must give both the same
alternate (ISO 19005-2, 6.2.4.4 t2).

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as nestedpattern_2b.py builds it."""
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
    Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([0, 1, 1, 0]), N=1)
)
spot = pdf.make_indirect(Array([Name.Separation, Name("/Spot"), Name.DeviceCMYK, tint]))


def ramp(components):
    """A one-in exponential function giving components outputs."""
    return Dictionary(
        FunctionType=2, Domain=Array([0, 1]), C0=Array([0] * components), C1=Array([1] * components), N=1
    )


def axial(colour_space, components):
    """An axial shading over colour_space whose function gives components."""
    return Dictionary(
        ShadingType=2,
        ColorSpace=colour_space,
        Coords=Array([0, 0, 100, 0]),
        Function=ramp(components),
    )


def shading_pattern(shading):
    """A shading pattern, which carries no /Resources (ISO 32000-1, Table 76)."""
    return Dictionary(Type=Name.Pattern, PatternType=2, Shading=shading)


p1 = pdf.make_indirect(shading_pattern(pdf.make_indirect(axial(spot, 1))))
p2 = shading_pattern(axial(Name.DeviceCMYK, 4))
spot2 = Array([Name.Separation, Name("/Spot2"), Name.DeviceCMYK, tint])
devicen = Array(
    [
        Name.DeviceN,
        Array([Name("/Spot2")]),
        Name.DeviceCMYK,
        tint,
        Dictionary(Colorants=Dictionary(Spot2=spot2)),
    ]
)
p3 = pdf.make_indirect(shading_pattern(axial(devicen, 1)))
spot_direct = Array([Name.Separation, Name("/Spot"), Name.DeviceCMYK, tint])
p5 = pdf.make_indirect(shading_pattern(axial(spot_direct, 1)))

form = Stream(pdf, b"q /Pattern cs /P5 scn 0 0 100 100 re f Q\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 100, 100])
form.Resources = Dictionary(Pattern=Dictionary(P5=p5))
form = pdf.make_indirect(form)


def add_page(content, resources):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 200, 200]),
            Resources=resources,
            Contents=pdf.make_indirect(Stream(pdf, content)),
        )
    )
    pdf.pages.append(pikepdf.Page(page))


add_page(
    b"q 1 0 0 rg 0 150 20 20 re f /Pattern cs /P1 scn 0 0 100 100 re f Q\n",
    Dictionary(Pattern=Dictionary(P1=p1)),
)
add_page(b"q /Pattern cs /P2 scn 0 0 100 100 re f Q\n", Dictionary(Pattern=Dictionary(P2=p2)))
add_page(b"q /Pattern cs /P3 scn 0 0 100 100 re f Q\n", Dictionary(Pattern=Dictionary(P3=p3)))
add_page(
    b"q /CS0 cs 1 scn 0 150 20 20 re f 0 0 0 1 k 30 150 20 20 re f /Pattern cs /P1 scn 0 0 100 100 re f Q\n",
    Dictionary(ColorSpace=Dictionary(CS0=spot), Pattern=Dictionary(P1=p1)),
)
add_page(b"q /F5 Do Q\n", Dictionary(XObject=Dictionary(F5=form)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "shadedefault_2b.pdf")
