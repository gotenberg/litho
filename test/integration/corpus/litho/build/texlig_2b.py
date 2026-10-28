#!/usr/bin/env python3
"""Build texlig_2b.pdf: a TeX-style ligature code and a code 32 whose name
maps nothing, in an embedded Type 1 font with no /ToUnicode.

ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
a code selects. TeX text fonts set the "fi" ligature at code 0x0C under a name
no glyph list holds, so the code has no Unicode value and reads as no
character: the word stays whole. PDF.js shows an unmapped code as itself and
reads 0x0C, a form feed, as a word space, which cuts the word in two. ISO
32000-1, 9.3.3 applies word spacing to the single-byte code 32 whatever glyph
it selects, so code 32 reads as a word space whatever its name.

Font F1 embeds a /FontFile Type 1 program on StandardEncoding holding the
letters it shows and T0C, and carries /Differences [12 /T0C 32 /T20] with no
/BaseEncoding. The program holds no T20, so code 32 selects .notdef, which
draws nothing, and /Widths gives it 0, so no gap separates the words it
divides: only the code can. The page shows two lines:

  veri <0C> cation <20> text   reads "verication text"
  code <20> space              reads "code space", the code-32 control

Read with every unmapped whitespace code as a space, the first line was
"veri cation text". A CMYK output intent covers the initial DeviceGray and the
XMP claims PDF/A-2b, so the planted glyph, width and Unicode gaps are what
fires.

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

LINES = [b"veri\x0ccation text", b"code space"]
letters = sorted({chr(c) for line in LINES for c in line if chr(c).isalpha()})
data, l1, l2, l3 = _type1.build(name=b"LithoTeX", glyphs=tuple(letters) + ("T0C",))
program = Stream(pdf, data)
program.Length1 = l1
program.Length2 = l2
program.Length3 = l3

descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoTeX"),
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
        BaseFont=Name("/LithoTeX"),
        FirstChar=0,
        LastChar=255,
        Widths=Array([0 if code == 32 else _type1.ADVANCE_WIDTH for code in range(256)]),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([12, Name("/T0C"), 32, Name("/T20")])),
        FontDescriptor=pdf.make_indirect(descriptor),
    )
)

body = b""
y = 700
for line in LINES:
    body += b"BT /F1 12 Tf 72 %d Td <%s> Tj ET\n" % (y, line.hex().encode())
    y -= 20
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
save(pdf, "texlig_2b.pdf", min_version="1.7")
