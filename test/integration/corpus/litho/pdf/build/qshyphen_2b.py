#!/usr/bin/env python3
"""Build qshyphen_2b.pdf: soft hyphens shown inside q...Q in a text object.

PDF.js does not advance its text position past a soft hyphen (U+00AD), so it
reads every glyph after one, until a text-positioning operator sets the
position again, the hyphen's advance before where ISO 32000-1, 9.4.4 puts it.
It saves its whole text state at q, text matrix included, and restores it at
Q, as litho's machine does: a soft hyphen shown inside q...Q leaves no shift
after Q, one shown before q keeps its shift after Q, and a Td inside q...Q is
undone. ISO 32000-1, 8.2, Figure 9 allows no q or Q inside a text object, so
no clause says what Q does to the text matrix there. No corpus file shows a
soft hyphen inside q...Q in a text object.

Font F1 embeds a Type 1 program holding the letters it shows and sfthyphen,
every glyph 600 units wide, and a /ToUnicode CMap that maps code AD to U+00AD.
At 12 points a glyph is 7.2 units wide, and the TJ numbers of -300 leave a
3.6-unit gap that PDF.js reads as a word space. The page shows five lines:

  (left) q (AD) Q [-300 (right)]           reads "left right"
  (outer AD) q (AD) Q [-300 (inner)]       reads "outer inner"
  (deep) q q (AD) Q (AD) Q [-300 (nest)]   reads "deep nest"
  (soft AD) q 0 -30 Td Q (ware)            reads "software"
  (plain) (AD) [-300 (text)]               reads "plain text", the control

Carrying the shift of a soft hyphen inside q...Q past Q closed the gap on the
first three lines; losing the shift of the one before q at the Td opened one
on the fourth. A CMYK output intent covers the initial DeviceGray and the XMP
claims PDF/A-2b, so nothing but the text fires at PDF/A-2b.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _type1
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, tounicode_cmap, xmp_packet

pdf = pikepdf.new()

NONSYMBOLIC = 32
SOFT_HYPHEN = 0xAD
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

LINES = [
    b"(left) Tj q (\xad) Tj Q [-300 (right)] TJ",
    b"(outer\xad) Tj q (\xad) Tj Q [-300 (inner)] TJ",
    b"(deep) Tj q q (\xad) Tj Q (\xad) Tj Q [-300 (nest)] TJ",
    b"(soft\xad) Tj q 0 -30 Td Q (ware) Tj",
    b"(plain) Tj (\xad) Tj [-300 (text)] TJ",
]
WORDS = b"left right outer inner deep nest software plain text"
letters = sorted({chr(c) for c in WORDS if chr(c).isalpha()})
data, l1, l2, l3 = _type1.build(name=b"LithoSoftHyphen", glyphs=tuple(letters) + ("sfthyphen",))
program = Stream(pdf, data)
program.Length1 = l1
program.Length2 = l2
program.Length3 = l3

descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoSoftHyphen"),
    Flags=NONSYMBOLIC,
    FontBBox=Array([0, -200, 550, 700]),
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontFile=pdf.make_indirect(program),
)
mapping = {ord(c): ord(c) for c in letters}
mapping[SOFT_HYPHEN] = SOFT_HYPHEN
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/LithoSoftHyphen"),
        FirstChar=0,
        LastChar=255,
        Widths=Array([_type1.ADVANCE_WIDTH] * 256),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([SOFT_HYPHEN, Name("/sfthyphen")])),
        FontDescriptor=pdf.make_indirect(descriptor),
        ToUnicode=tounicode_cmap(pdf, mapping),
    )
)

body = b""
y = 700
for line in LINES:
    body += b"BT /F1 12 Tf 72 %d Td %s ET\n" % (y, line)
    y -= 40

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "qshyphen_2b.pdf", min_version="1.7")
