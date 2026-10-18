#!/usr/bin/env python3
"""Build patternglyph_2b.pdf: Type 3 glyphs a tiling pattern's cell shows.

A tiling pattern's cell is a content stream (ISO 32000-1, 8.7.3.1), so it can
show text in a Type 3 font, whose glyph descriptions are content streams in
turn (9.6.5) and can paint images, show text in other fonts, select colours and
select patterns of their own. veraPDF 1.30.2 judges what such a glyph draws
once an scn selects the pattern and text is shown with the font. No corpus file
shows Type 3 text in a pattern cell, so this fixture plants one path per page,
each answering a rule nothing else in the file fires:

| Page | Path                                                    | Glyph draws                     | Answers at PDF/A-2b |
| ---- | ------------------------------------------------------- | ------------------------------- | ------------------- |
| 1    | tiling cell, Type 3 glyph                               | an image with `/Interpolate true` | `6.2.8/3`           |
| 2    | tiling cell, Type 3 glyph                               | text in an unembedded TrueType  | `6.2.11.4.1/1`      |
| 3    | tiling cell, Type 3 glyph, tiling cell, Type 3 glyph    | a DeviceRGB fill                | `6.2.4.3/2`         |
| 4    | tiling cell selecting a Type 3 font with Tf, no text    | an image with `/Intent /Bogus`  | silent              |

Page 4 is the must-not-fire control: veraPDF reaches a Type 3 glyph through
the text that shows it, never through a Tf alone, so `6.2.6/1` stays silent.

A CMYK OutputIntent covers the DeviceGray every cell and glyph shows text in.

See ../README.md for the authoritative expected firing set.
"""

import struct
import zlib

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, unembedded_truetype, xmp_packet

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


def gray_image(**extra):
    """A 2x2 DeviceGray image, which the CMYK OutputIntent covers."""
    img = Stream(pdf, zlib.compress(bytes([0, 85, 170, 255])))
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 2
    img.Height = 2
    img.ColorSpace = Name.DeviceGray
    img.BitsPerComponent = 8
    img.Filter = Name.FlateDecode
    for key, value in extra.items():
        img[Name("/" + key)] = value
    return pdf.make_indirect(img)


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


SHOW = b"q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n"

glyph1 = type3(
    b"q 1000 0 0 1000 0 0 cm /Im1 Do Q\n",
    Dictionary(XObject=Dictionary(Im1=gray_image(Interpolate=True))),
)
cell1 = tiling(SHOW, Dictionary(Font=Dictionary(T3=glyph1)))

glyph2 = type3(
    b"BT /TT 12 Tf 0 0 Td (AB) Tj ET\n",
    Dictionary(Font=Dictionary(TT=pdf.make_indirect(unembedded_truetype()))),
)
cell2 = tiling(SHOW, Dictionary(Font=Dictionary(T3=glyph2)))

inner_glyph = type3(b"1 0 0 rg 0 0 1000 1000 re f\n", Dictionary())
inner_cell = tiling(SHOW, Dictionary(Font=Dictionary(T3=inner_glyph)))
outer_glyph = type3(
    b"q /Pattern cs /P0 scn 0 0 1000 1000 re f Q\n",
    Dictionary(Pattern=Dictionary(P0=inner_cell)),
)
cell3 = tiling(SHOW, Dictionary(Font=Dictionary(T3=outer_glyph)))

glyph4 = type3(
    b"q 1000 0 0 1000 0 0 cm /Im4 Do Q\n",
    Dictionary(XObject=Dictionary(Im4=gray_image(Intent=Name("/Bogus")))),
)
cell4 = tiling(b"BT /T3 10 Tf ET 0 0 20 20 re f\n", Dictionary(Font=Dictionary(T3=glyph4)))


def add_page(cell):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 200, 200]),
            Resources=Dictionary(Pattern=Dictionary(P1=cell)),
            Contents=pdf.make_indirect(Stream(pdf, b"q /Pattern cs /P1 scn 0 0 100 100 re f Q\n")),
        )
    )
    pdf.pages.append(pikepdf.Page(page))


for cell in (cell1, cell2, cell3, cell4):
    add_page(cell)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "patternglyph_2b.pdf")
