#!/usr/bin/env python3
"""Build patternshade_2b.pdf: shadings a pattern or a Type 3 glyph reaches.

A shading reaches the page through a shading pattern an scn selects (ISO
32000-1, 8.7.4.1) or through the sh operator (8.7.4.2), and either can sit in
the cell of a tiling pattern (8.7.3.1) or in a Type 3 glyph description
(9.6.5). veraPDF 1.30.2 judges the colour space of every such shading, the
tint transform of a Separation or DeviceN space included, and judges neither
the shading's /Function nor a shading pattern's own /ExtGState. No corpus file
reaches a shading through a cell or a glyph, so this fixture plants one per
path, each answering a rule nothing else in the file fires:

| Page | Path                                 | Shading colour space               | Answers at PDF/A-2b     |
| ---- | ------------------------------------ | ---------------------------------- | ----------------------- |
| 1    | tiling cell, `/SH1 scn`              | DeviceRGB                          | `6.2.4.3/2`             |
| 2    | Type 3 glyph on the page, `/SH2 scn` | Separation, type 4 tint 2147483648 | `6.1.13/1`              |
| 3    | tiling cell, Type 3 glyph, `/Sh3 sh` | DeviceN of 33 colorants            | `6.1.13/9`, `6.2.4.4/1` |

Every shading's /Function is a type 4 function whose program holds 2147483649
and 40000.5, and SH1 carries an /ExtGState setting /TR /Identity and /BM
/Foo: veraPDF judges none of them, so they are must-not-fire controls for
6.2.5 and 6.2.10, keep 6.1.13 t1 at the tint's single count, and keep 6.1.12
t2 silent at PDF/A-1b.

A CMYK OutputIntent covers DeviceCMYK and the DeviceGray a glyph shows in.

See ../README.md for the authoritative expected firing set.
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


def type4(program, inputs, outputs):
    """A PostScript calculator function of the given arity."""
    fn = Stream(pdf, program)
    fn.FunctionType = 4
    fn.Domain = Array([0, 1] * inputs)
    fn.Range = Array([0, 1] * outputs)
    return pdf.make_indirect(fn)


def shading(colour_space):
    """An axial shading whose /Function only veraPDF's model leaves unread."""
    return pdf.make_indirect(
        Dictionary(
            ShadingType=2,
            ColorSpace=colour_space,
            Coords=Array([0, 0, 20, 0]),
            Function=type4(b"{ 2147483649 pop 40000.5 pop }", 1, 1),
            Extend=Array([True, True]),
        )
    )


def tiling(content, resources):
    """A coloured tiling pattern carrying its own /Resources."""
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = 1
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 20, 20])
    pat.XStep = 20
    pat.YStep = 20
    pat.Resources = resources
    return pdf.make_indirect(pat)


def type3(glyph, resources):
    """A one-glyph Type 3 font; the glyph sets its colour (d0)."""
    proc = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n" + glyph))
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(A=proc),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A])),
            FirstChar=65,
            LastChar=65,
            Widths=Array([1000]),
            Resources=resources,
        )
    )


sh1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Pattern,
        PatternType=2,
        Shading=shading(Name.DeviceRGB),
        ExtGState=Dictionary(Type=Name.ExtGState, TR=Name.Identity, BM=Name("/Foo")),
    )
)
cell1 = tiling(b"q /Pattern cs /SH1 scn 0 0 20 20 re f Q\n", Dictionary(Pattern=Dictionary(SH1=sh1)))

sh2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Pattern,
        PatternType=2,
        Shading=shading(
            Array(
                [
                    Name.Separation,
                    Name("/Spot2"),
                    Name.DeviceCMYK,
                    type4(b"{ 2147483648 pop dup dup dup }", 1, 4),
                ]
            )
        ),
    )
)
glyph2 = type3(b"q /Pattern cs /SH2 scn 0 0 1000 1000 re f Q\n", Dictionary(Pattern=Dictionary(SH2=sh2)))

colorants = Array([Name("/C%02d" % i) for i in range(33)])
sh3 = shading(
    Array(
        [
            Name.DeviceN,
            colorants,
            Name.DeviceCMYK,
            type4(b"{ " + b"pop " * 33 + b"0 0 0 0 }", 33, 4),
        ]
    )
)
glyph3 = type3(b"q /Sh3 sh Q\n", Dictionary(Shading=Dictionary(Sh3=sh3)))
cell3 = tiling(b"q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n", Dictionary(Font=Dictionary(T3=glyph3)))


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


add_page(b"q /Pattern cs /P1 scn 0 0 100 100 re f Q\n", Dictionary(Pattern=Dictionary(P1=cell1)))
add_page(b"q 0 g BT /T2 50 Tf 10 10 Td (A) Tj ET Q\n", Dictionary(Font=Dictionary(T2=glyph2)))
add_page(b"q /Pattern cs /P3 scn 0 0 100 100 re f Q\n", Dictionary(Pattern=Dictionary(P3=cell3)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "patternshade_2b.pdf")
