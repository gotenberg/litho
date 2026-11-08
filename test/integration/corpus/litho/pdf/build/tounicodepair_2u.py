#!/usr/bin/env python3
"""Build tounicodepair_2u.pdf: a simple font whose /ToUnicode CMap declares a
two-byte codespace and maps a byte pair of the shown string.

ISO 32000-1, 9.6.1 makes a simple font's codes one byte each, so a /ToUnicode
CMap whose codespace is two bytes wide does not make a byte pair one code.
PDF.js keys a /ToUnicode map by the code's value and looks each byte up there,
so <006C> maps code 6C; veraPDF looks a simple font's code up by its value
too. A byte pair the CMap maps, <6C6C> or <6F20>, names no code of the font.

Font F1 embeds a /FontFile Type 1 program on StandardEncoding holding the
glyphs it shows, and its /ToUnicode CMap declares <0000> to <FFFF>, maps every
shown code by its two-byte value, and maps the pairs <6C6C> to "X" and <6F20>
to "Y". The page shows "Hello world":

  read a byte a code         "Hello world"
  read two bytes a mapped pair  "HeXYworld"

A CMYK output intent covers the initial DeviceGray and the XMP claims PDF/A-2u,
so every shown code has a Unicode value and none of the font rules fire.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _type1
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

NONSYMBOLIC = 32
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

LINE = b"Hello world"
GLYPH_NAMES = {ord(" "): "space"}
codes = sorted(set(LINE))
glyphs = tuple(GLYPH_NAMES.get(c, chr(c)) for c in codes)
data, l1, l2, l3 = _type1.build(name=b"LithoPair", glyphs=glyphs)
program = Stream(pdf, data)
program.Length1 = l1
program.Length2 = l2
program.Length3 = l3

PAIRS = {b"ll": "X", b"o ": "Y"}
cmap_lines = [
    "/CIDInit /ProcSet findresource begin",
    "12 dict begin",
    "begincmap",
    "/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def",
    "/CMapName /Adobe-Identity-UCS def",
    "/CMapType 2 def",
    "1 begincodespacerange",
    "<0000> <FFFF>",
    "endcodespacerange",
    "%d beginbfchar" % (len(codes) + len(PAIRS)),
]
for code in codes:
    cmap_lines.append("<%04X> <%04X>" % (code, code))
for pair in sorted(PAIRS):
    cmap_lines.append("<%s> <%04X>" % (pair.hex().upper(), ord(PAIRS[pair])))
cmap_lines += [
    "endbfchar",
    "endcmap",
    "CMapName currentdict /CMap defineresource pop",
    "end",
    "end",
]
to_unicode = pdf.make_indirect(Stream(pdf, ("\n".join(cmap_lines) + "\n").encode("ascii")))

descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoPair"),
    Flags=NONSYMBOLIC,
    FontBBox=Array([0, -200, 550, 700]),
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontFile=pdf.make_indirect(program),
)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/LithoPair"),
        FirstChar=0,
        LastChar=255,
        Widths=Array([_type1.ADVANCE_WIDTH] * 256),
        ToUnicode=to_unicode,
        FontDescriptor=pdf.make_indirect(descriptor),
    )
)

body = b"BT /F1 12 Tf 72 700 Td <%s> Tj ET\n" % LINE.hex().encode()
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="U"))
save(pdf, "tounicodepair_2u.pdf", min_version="1.7")
