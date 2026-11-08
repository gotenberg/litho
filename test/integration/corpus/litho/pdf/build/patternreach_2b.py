#!/usr/bin/env python3
"""Build patternreach_2b.pdf: DeviceRGB a pattern reaches, one path per page.

A pattern reaches a page through an scn or SCN that selects it (ISO 32000-1,
8.7.3.1 and 8.7.4.1), from the page's content, a form XObject, an annotation
appearance, a Type 3 glyph or another pattern's cell, and a shading reaches it
through a shading pattern or through sh (8.7.4.2). veraPDF 1.30.2 judges the
colour space of each such shading and of each colour operator in a cell
(PDShadingPattern/shading/colorSpace, PDTilingPattern/contentStream), and
judges a stream once however many paths reach it. No corpus file isolates
these paths: the veraPDF corpus reaches a cell only from a page's content and
reaches no shading pattern, and the litho fixtures before this one reach a
shading pattern only from a page, a cell or a glyph. This fixture plants one path
per page, each painting DeviceRGB, which ISO 19005-2, 6.2.4.3 t2 answers under
the CMYK OutputIntent:

| Page | Path                                                         | `6.2.4.3/2` |
| ---- | ------------------------------------------------------------ | ----------- |
| 1    | `SCN` strokes with a shading pattern                         | 1           |
| 2    | a form XObject selects a shading pattern                     | 1           |
| 3    | an annotation appearance selects a shading pattern           | 1           |
| 4    | text in a `d1` Type 3 font filled with a shading pattern     | 1           |
| 5    | a tiling cell paints a form XObject that runs `sh`           | 1           |
| 6    | a shading pattern over a type 4 mesh shading                 | 1           |
| 7    | a shading pattern over an `/Indexed` space on DeviceRGB      | 1           |
| 8    | a direct shading pattern holding a direct shading            | 2           |
| 9    | a tiling cell that selects itself                            | 2           |
| 10   | two tiling cells that select each other                      | 4           |
| 11   | a Type 3 glyph whose pattern cell shows the same font        | 2           |
| 12   | one shading pattern on the page and in a cell it paints      | 1           |
| 13   | one /Pattern dictionary two cells share                      | 1           |

A cycle ends where it reaches a stream already judged (pages 9 to 11), and a
shading pattern or a /Pattern dictionary reached twice counts once (pages 12
and 13). Page 8's pattern and shading are direct objects, so the `scn` and the
fill that follows it are each a link to the shading's colour space.

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

RGB = Name.DeviceRGB
SELECT = b"q /Pattern cs /S scn 0 0 20 20 re f Q\n"
SELECT_CELL = b"q /Pattern cs /P scn 0 0 100 100 re f Q\n"


def exponential(components):
    """A type 2 function from one input to components outputs."""
    return Dictionary(
        FunctionType=2,
        Domain=Array([0, 1]),
        C0=Array([0] * components),
        C1=Array([1] * components),
        N=1,
    )


def axial(colour_space, components=3):
    """An axial shading over colour_space."""
    return pdf.make_indirect(
        Dictionary(
            ShadingType=2,
            ColorSpace=colour_space,
            Coords=Array([0, 0, 20, 0]),
            Function=exponential(components),
        )
    )


def shading_pattern(shading):
    return pdf.make_indirect(Dictionary(Type=Name.Pattern, PatternType=2, Shading=shading))


def rgb_patterns():
    """Resources naming /S, a shading pattern over a DeviceRGB shading."""
    return Dictionary(Pattern=Dictionary(S=shading_pattern(axial(RGB))))


def form(content, resources):
    xobj = Stream(pdf, content)
    xobj.Type = Name.XObject
    xobj.Subtype = Name.Form
    xobj.BBox = Array([0, 0, 100, 100])
    xobj.Resources = resources
    return pdf.make_indirect(xobj)


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


def type3(glyph, resources, metrics=b"1000 0 d0\n"):
    """A one-glyph Type 3 font."""
    proc = pdf.make_indirect(Stream(pdf, metrics + glyph))
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


def annotation(appearance):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Square,
            Rect=Array([0, 0, 100, 100]),
            F=4,
            AP=Dictionary(N=appearance),
        )
    )


def add_page(content, resources, annots=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=resources,
        Contents=pdf.make_indirect(Stream(pdf, content)),
    )
    if annots is not None:
        page.Annots = annots
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


# 1: a stroke.
add_page(b"q /Pattern CS /S SCN 0 0 50 50 re S Q\n", rgb_patterns())

# 2: a form XObject.
add_page(b"q /X Do Q\n", Dictionary(XObject=Dictionary(X=form(SELECT, rgb_patterns()))))

# 3: an annotation appearance.
add_page(b"q 0 g 0 0 1 1 re f Q\n", Dictionary(), Array([annotation(form(SELECT, rgb_patterns()))]))

# 4: text filled with the pattern, in a font whose glyph takes its colour (d1).
page4 = rgb_patterns()
page4.Font = Dictionary(F=type3(b"0 0 1000 1000 re f\n", Dictionary(), b"1000 0 0 0 1000 1000 d1\n"))
add_page(b"q /Pattern cs /S scn BT /F 50 Tf 10 10 Td (A) Tj ET Q\n", page4)

# 5: sh in a form a cell paints.
sh_form = form(b"q /Sh sh Q\n", Dictionary(Shading=Dictionary(Sh=axial(RGB))))
add_page(SELECT_CELL, Dictionary(Pattern=Dictionary(P=tiling(b"q /X Do Q\n", Dictionary(XObject=Dictionary(X=sh_form))))))

# 6: a free-form triangle mesh, three vertices of flag, x, y and r g b.
mesh = Stream(
    pdf,
    bytes([0, 0, 0, 255, 0, 0, 0, 255, 0, 0, 255, 0, 0, 0, 255, 0, 0, 255, 255, 0, 0]),
)
mesh.ShadingType = 4
mesh.ColorSpace = RGB
mesh.BitsPerCoordinate = 8
mesh.BitsPerComponent = 8
mesh.BitsPerFlag = 8
mesh.Decode = Array([0, 255, 0, 255, 0, 1, 0, 1, 0, 1])
add_page(SELECT, Dictionary(Pattern=Dictionary(S=shading_pattern(pdf.make_indirect(mesh)))))

# 7: /Indexed over DeviceRGB.
indexed = Array([Name.Indexed, RGB, 1, String(b"\x00\x00\x00\xff\xff\xff")])
add_page(SELECT, Dictionary(Pattern=Dictionary(S=shading_pattern(axial(indexed, 1)))))

# 8: a direct shading pattern over a direct shading.
direct = Dictionary(
    Type=Name.Pattern,
    PatternType=2,
    Shading=Dictionary(ShadingType=2, ColorSpace=RGB, Coords=Array([0, 0, 20, 0]), Function=exponential(3)),
)
add_page(SELECT, Dictionary(Pattern=Dictionary(S=direct)))

# 9: a cell that selects itself.
self_cell = tiling(b"q 1 0 0 rg 0 0 5 5 re f /Pattern cs /P scn 0 0 20 20 re f Q\n", Dictionary())
self_cell.Resources = Dictionary(Pattern=Dictionary(P=self_cell))
add_page(SELECT_CELL, Dictionary(Pattern=Dictionary(P=self_cell)))

# 10: two cells that select each other.
cell_a = tiling(b"q 1 0 0 rg 0 0 5 5 re f /Pattern cs /Q scn 0 0 20 20 re f Q\n", Dictionary())
cell_b = tiling(b"q 0 1 0 rg 0 0 5 5 re f /Pattern cs /P scn 0 0 20 20 re f Q\n", Dictionary())
cell_a.Resources = Dictionary(Pattern=Dictionary(Q=cell_b))
cell_b.Resources = Dictionary(Pattern=Dictionary(P=cell_a))
add_page(SELECT_CELL, Dictionary(Pattern=Dictionary(P=cell_a)))

# 11: a glyph whose cell shows the glyph's own font.
cycle_font = type3(b"q 1 0 0 rg 0 0 100 100 re f /Pattern cs /P scn 0 0 1000 1000 re f Q\n", Dictionary())
cycle_cell = tiling(b"q 0 g BT /T 10 Tf 1 1 Td (A) Tj ET Q\n", Dictionary(Font=Dictionary(T=cycle_font)))
cycle_font.Resources = Dictionary(Pattern=Dictionary(P=cycle_cell))
add_page(b"q 0 g BT /T 50 Tf 10 10 Td (A) Tj ET Q\n", Dictionary(Font=Dictionary(T=cycle_font)))

# 12: one shading pattern on the page and in a cell the page paints.
shared_pattern = shading_pattern(axial(RGB))
shared_cell = tiling(SELECT, Dictionary(Pattern=Dictionary(S=shared_pattern)))
add_page(
    b"q /Pattern cs /S scn 0 0 20 20 re f /P scn 0 0 100 100 re f Q\n",
    Dictionary(Pattern=Dictionary(S=shared_pattern, P=shared_cell)),
)

# 13: one /Pattern dictionary two cells share.
shared_dict = pdf.make_indirect(Dictionary(S=shading_pattern(axial(RGB))))
add_page(
    b"q /Pattern cs /A scn 0 0 50 50 re f /B scn 50 50 50 50 re f Q\n",
    Dictionary(
        Pattern=Dictionary(
            A=tiling(SELECT, Dictionary(Pattern=shared_dict)),
            B=tiling(SELECT, Dictionary(Pattern=shared_dict)),
        )
    ),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "patternreach_2b.pdf")
