#!/usr/bin/env python3
"""Build usedcs_2b.pdf: which resources count as colour usage at PDF/A-2b.

ISO 19005-2, 6.2.4.3 is usage-driven: a colour space is constrained when content
paints with it, not when a resource dictionary merely declares it. Three resource
categories carry their own colour and each is reached by its own operator, so
each needs its own usage gate:

| Resource            | Painted by            | Declared-only means |
| ------------------- | --------------------- | ------------------- |
| `/Pattern` entry    | a name operand of `scn` | paints nothing    |
| `/Shading` entry    | `sh`                  | paints nothing      |
| `/Font` Type3 entry | selected by `Tf`      | draws nothing       |

A fourth construct is not about usage at all. An uncoloured tiling pattern
(`/PaintType 2`) shall not specify any colour in its content stream: the colour
comes from the operands of the `scn` that selects it, interpreted in the
underlying space of `[/Pattern base]` (ISO 32000-1 §8.7.3.3, Table 75). The
initial DeviceGray of §8.6.8 therefore never applies inside the cell, so a bare
painting operator there is not a DeviceGray use. Measured: veraPDF reports
nothing for such a cell, not even for an explicit device colour written into it.

Seven objects sit in one file. Only `Sh1` and `T3Used` may report, which is the
masking probe: five of the seven are controls, and each is the kind of object a
naive walk condemns.

| Object   | Construct                                          | Rule        |
| -------- | -------------------------------------------------- | ----------- |
| `Sh1`    | shading painted by `sh`, `/ColorSpace /DeviceGray`  | `6.2.4.3/4` |
| `T3Used` | Type3 font selected by `Tf`, CharProcs paint `0 g`   | `6.2.4.3/4` |
| `Sh0`    | DeviceCMYK shading, declared but never painted       | silent      |
| `T3Unused` | Type3 font declared but never selected            | silent      |
| `PUnc`   | uncoloured pattern, explicit `0 0 0 1 k` in its cell | silent      |
| `PUnref` | coloured pattern, DeviceCMYK, never named by `scn`  | silent      |
| `PCol`   | coloured pattern selecting its own ICCBased space   | silent      |

Every declared-only object paints DeviceCMYK rather than DeviceGray on purpose,
and the two painted ones paint DeviceGray. Without that split the file proves
nothing: `Sh1` and `T3Used` legitimately report test 4, so an implementation that
also scans `Sh0`, `T3Unused`, `PUnc` and `PUnref` still reports test 4 and agrees
with veraPDF for the wrong reasons. Measured on the first build, and fixed by
moving every ungated source to test 3, which nothing conforming produces.

The file declares no OutputIntent, which is what keeps test 4 reachable: the test
is `gOutputCS != null`, so an OutputIntent of any destination colour space
permits DeviceGray outright. The page paints its own text inside an ICCBased
scope, because text drawn with no colour space selected is itself a DeviceGray
use and would report the clause from the page, masking every object above.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(colour_space, components, tag):
    """A structurally minimal ICC profile veraPDF will parse without throwing."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = components
    return pdf.make_indirect(stream)


iccbased = pdf.make_indirect(Array([Name.ICCBased, icc_profile(b"RGB ", 3, b"ICCB")]))


def shading(colour_space=Name.DeviceGray, components=1):
    """An axial shading in the given device colour space."""
    return pdf.make_indirect(
        Dictionary(
            ShadingType=2,
            ColorSpace=colour_space,
            Coords=Array([0, 0, 100, 0]),
            Function=pdf.make_indirect(
                Dictionary(
                    FunctionType=2,
                    Domain=Array([0, 1]),
                    C0=Array([0] * components),
                    C1=Array([1] * components),
                    N=1,
                )
            ),
        )
    )


def tiling(content, paint_type=1):
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = paint_type
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 20, 20])
    pat.XStep = 20
    pat.YStep = 20
    pat.Resources = Dictionary(ColorSpace=Dictionary(CS0=iccbased))
    return pdf.make_indirect(pat)


def type3_font(paint=b"0 g"):
    """A Type3 font whose only glyph procedure paints a device colour."""
    proc = Stream(pdf, b"10 0 0 0 10 10 d1 " + paint + b" 0 0 10 10 re f\n")
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 10, 10]),
            FontMatrix=Array([0.1, 0, 0, 0.1, 0, 0]),
            CharProcs=Dictionary(a=pdf.make_indirect(proc)),
            Encoding=Dictionary(
                Type=Name.Encoding, Differences=Array([97, Name("/a")])
            ),
            FirstChar=97,
            LastChar=97,
            Widths=Array([10]),
            Resources=Dictionary(),
        )
    )


helvetica = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# The underlying space an uncoloured pattern is selected through.
uncoloured_cs = pdf.make_indirect(Array([Name.Pattern, Name.DeviceRGB]))

content = (
    b"q /CS0 cs 0.2 0.4 0.6 scn\n"
    # The page text sits inside the ICCBased scope so the page adds no DeviceGray.
    b"  BT /F1 12 Tf 50 740 Td (resource usage and colour) Tj ET\n"
    b"Q\n"
    # Sh1 is painted; Sh0 is not named anywhere.
    b"q 1 0 0 1 60 620 cm /Sh1 sh Q\n"
    # T3Used is selected by Tf; T3Unused is never selected.
    b"q BT /T3Used 12 Tf 60 560 Td (a) Tj ET Q\n"
    # An uncoloured pattern, selected through [/Pattern /DeviceRGB]. Its cell
    # writes an explicit device colour, which the spec forbids and veraPDF
    # ignores: the colour comes from the scn operands, not from the cell.
    b"q /PUCS cs 0.2 0.4 0.6 /PUnc scn 60 480 100 40 re f Q\n"
    # A coloured pattern selecting its own permitted space.
    b"q /Pattern cs /PCol scn 200 480 100 40 re f Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(
                F1=helvetica,
                T3Used=type3_font(),
                # Declared but never selected by any Tf. It paints DeviceCMYK
                # so that scanning it by mistake reports test 3, not test 4.
                T3Unused=type3_font(b"0 0 0 1 k"),
            ),
            ColorSpace=Dictionary(CS0=iccbased, PUCS=uncoloured_cs),
            Shading=Dictionary(
                # Painted by the sh above.
                Sh1=shading(),
                # Declared but never painted. DeviceCMYK so that scanning it by
                # mistake reports test 3, not test 4.
                Sh0=shading(Name.DeviceCMYK, 4),
            ),
            Pattern=Dictionary(
                PUnc=tiling(b"0 0 0 1 k 0 0 20 20 re f\n", paint_type=2),
                PCol=tiling(b"/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n"),
                # Declared but never named by any scn. DeviceCMYK so that
                # scanning it by mistake reports test 3, not test 4.
                PUnref=tiling(b"0 0 0 1 k 0 0 20 20 re f\n"),
            ),
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata.
save(pdf, "usedcs_2b.pdf", min_version="1.7")
