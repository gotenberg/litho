#!/usr/bin/env python3
"""Build iccv4intent_1b.pdf: an output intent carrying an ICC v4 profile, at
PDF/A-1b.

PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO 19005-1, 6.2.2 takes
the destination profile PDF Reference 1.4, 9.10.4 describes, an ICC.1:1998-09
(v2) profile, and veraPDF's test reads "version < 3.0". ISO 19005-2, -3 and -4,
6.2.3 admit a v4 profile. litho accepted major versions 2 and 4 in every part,
so a v4 destination profile passed PDF/A-1 where veraPDF fails it. No file of
the litho or veraPDF corpora reaches it; the pdfjs and govdocs files that do
are not remediated by the gate.

The file is otherwise a conforming PDF/A-1b document: a 1.4 header, an XMP
claim, a CMYK printer profile in the one output intent, and its text in the
conforming embedded TrueType of baretree_2b.pdf, in the initial DeviceGray the
CMYK intent covers. So at PDF/A-1a and 1b the profile version is the verdict,
and at the later parts the file is as conforming as their claims allow.

The profile is the structurally minimal header iccbased_4.py builds, with the
version field set to 4.2.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_4_2 = 0x04200000


def icc_profile(device_class, colour_space, version, tag):
    """A structurally minimal ICC profile, as iccbased_4.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", version)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", ICC_VERSION_4_2, b"OUT4"))
destination.N = 4
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho v4 destination"),
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
    FontName=Name("/LithoBareSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoBareSans"),
        FirstChar=0x41,
        LastChar=0x42,
        Widths=Array([_truetype.ADVANCE_WIDTH] * 2),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, b"BT /F1 24 Tf 20 100 Td (AB) Tj ET\n")),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))
save(pdf, "iccv4intent_1b.pdf", min_version="1.4", fix_metadata_version=False)
