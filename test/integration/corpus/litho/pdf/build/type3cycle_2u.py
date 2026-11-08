#!/usr/bin/env python3
"""Build type3cycle_2u.pdf: a Type 3 font shown again in a cell its own glyph selects.

A Type 3 glyph procedure is a content stream (ISO 32000-1, 9.6.5) and can
select a tiling pattern whose cell (8.7.3.1) shows text in the same font.
veraPDF 1.30.2 judges the glyphs the cell shows as any other, and tells them
apart from the page's by the text rendering mode the cell starts in, the mode
the selecting procedure starts in. No corpus file the gate validates reaches
the shape, so this fixture plants it with the cell's glyph as the only one
without a Unicode value:

| Stream                         | Shows                                   | Unicode value      |
| ------------------------------ | --------------------------------------- | ------------------ |
| page                           | code A, glyph `A`, rendering mode 0     | `A`, from the AGL  |
| procedure of `A`               | selects the cell's pattern and fills    |                    |
| cell                           | code B, glyph `r1`, of the same font    | none               |

ISO 19005-2, 6.2.11.7.2 t1 (6.3.8 t1 at PDF/A-1a, ISO 14289-1, 7.21.7 t1 and
ISO 14289-2, 8.4.5.8 t1 at PDF/UA) fails once, for `r1`; litho once judged
no glyph of a Type 3 font in a stream its own procedures reach, which left the
rule silent outside PDF/A-1a.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

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

glyph_a = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n/Pattern cs /P scn 0 0 1000 1000 re f\n"))
glyph_r1 = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n0 0 1000 1000 re f\n"))
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(A=glyph_a, r1=glyph_r1),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A, Name("/r1")])),
        FirstChar=65,
        LastChar=66,
        Widths=Array([1000, 1000]),
    )
)

cell = Stream(pdf, b"BT /T 4 Tf (B) Tj ET\n")
cell.Type = Name.Pattern
cell.PatternType = 1
cell.PaintType = 1
cell.TilingType = 1
cell.BBox = Array([0, 0, 20, 20])
cell.XStep = 20
cell.YStep = 20
cell.Resources = Dictionary(Font=Dictionary(T=font))
font.Resources = Dictionary(Pattern=Dictionary(P=pdf.make_indirect(cell)))

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(Font=Dictionary(T=font)),
        Contents=pdf.make_indirect(Stream(pdf, b"BT /T 10 Tf 10 10 Td (A) Tj ET\n")),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="U"))
save(pdf, "type3cycle_2u.pdf")
