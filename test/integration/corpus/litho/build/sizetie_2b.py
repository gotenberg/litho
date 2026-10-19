#!/usr/bin/env python3
"""Build sizetie_2b.pdf: two font sizes of equal weight, under bookmark
generation, at PDF/A-2b.

Bookmark generation takes the body font size to be the size that sets the
most characters, and calls a line a heading when it is set larger. Two sizes
setting the same number of characters tie, and the tie used to break in the
iteration order of a Go map, which changes from run to run: the outline came
out with the heading or without it. No litho fixture held such a tie, so the
gate's determinism cell, which runs each bookmark generation several times,
never saw it.

One page shows `AABB` at 48 points over `ABAB` at 12, four characters each.
The tie breaks to the smaller size, so the body is 12 points and `AABB` is the
one heading.

The file conforms to PDF/A-2b: the text is in the conforming embedded
TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
covers, as in pagelabels_2b.pdf, so no colour or font clause fires.

See ../README.md for the authoritative expected firing set.
"""

import struct

import _truetype
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

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoTieSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoTieSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)

# Four characters at each size: the weights tie.
content = Stream(
    pdf,
    b"BT /F1 48 Tf 72 600 Td (AABB) Tj ET\n"
    b"BT /F1 12 Tf 72 500 Td (ABAB) Tj ET\n",
)
pdf.pages.append(
    pikepdf.Page(
        pdf.make_indirect(
            Dictionary(
                Type=Name.Page,
                MediaBox=Array([0, 0, 612, 792]),
                Resources=Dictionary(Font=Dictionary(F1=font)),
                Contents=pdf.make_indirect(content),
            )
        )
    )
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "sizetie_2b.pdf", min_version="1.7")
