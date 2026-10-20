#!/usr/bin/env python3
"""Build iccbadcmyk_2b.pdf: two output intents sharing one CMYK ICC profile
of the scanner device class, at PDF/A-2b.

ISO 19005-2, 6.2.3 asks the destination profile of a PDF/A output intent to
be an output (prtr) or display (mntr) profile, and ISO 19005-1, 6.2.2 and
19005-4, 6.2.3 ask the same, so every level fails this file, whose profile
header declares the input device class scnr.

Remediation replaces the profile with a stock profile of the same data colour
space. The header names CMYK, so the replacement is litho's stock CMYK
printer profile (graphics/icc/profiles/cgats001-cmyk-v2.icc), not sRGB: an
RGB destination under a CMYK output condition would stop covering the
DeviceCMYK content the page paints (ISO 19005-2, 6.2.4.3).

An ICCBased space admits a scanner profile (ISO 19005-2, 6.2.4.2), and the
page paints one over the same profile stream, so remediation keeps the source
profile for that space and points the intents at a new profile object. The
GTS_PDFA1 and GTS_PDFX intents name one profile, and ISO 19005-2, 6.2.3 t2
asks every intent's /DestOutputProfile to be one indirect object, so both
must name one replacement. iccbadcmyk_1b.pdf takes the in-place branch.

The profile is a version 2.1 output profile in every other respect: desc as
textDescriptionType, cprt as textType, the D50 media white in wtpt, and lut8
A2B0 and B2A0 transforms over a two-point grid. Only the device class is
wrong. The page paints a DeviceCMYK square, an ICCBased square and its text
in the conforming embedded TrueType of baretree_2b.pdf.

See ../README.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_VERSION_2_1 = 0x02100000
D50 = (0.9642, 1.0, 0.8249)
IDENTITY = (1, 0, 0, 0, 1, 0, 0, 0, 1)


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def text_description(text):
    """A v2 textDescriptionType (ICC.1:2001-04, 6.5.17): ASCII only."""
    ascii_text = text.encode("ascii") + b"\0"
    return b"desc" + bytes(4) + struct.pack(">I", len(ascii_text)) + ascii_text + bytes(4 + 4 + 2 + 1 + 67)


def lut8(inputs, outputs, clut):
    """A lut8Type (ICC.1:2001-04, 6.5.8) over a two-point grid, with identity
    input and output tables."""
    ramp = bytes(range(256))
    return b"mft1" + bytes(4) + bytes([inputs, outputs, 2, 0]) + s15(*IDENTITY) + ramp * inputs + bytes(clut) + ramp * outputs


def a2b0():
    """CMYK to Lab: lightness falls with total ink, a and b stay neutral."""
    clut = []
    for c in (0, 1):
        for m in (0, 1):
            for y in (0, 1):
                for k in (0, 1):
                    clut += [max(0, 255 - 64 * (c + m + y + 2 * k)), 128, 128]
    return lut8(4, 3, clut)


def b2a0():
    """Lab to CMYK: dark colours take black ink only."""
    clut = []
    for lightness in (0, 1):
        for _a in (0, 1):
            for _b in (0, 1):
                clut += [0, 0, 0, 0 if lightness else 255]
    return lut8(3, 4, clut)


def icc_profile(version, device_class, tags):
    """A CMYK ICC profile of tags, (signature, data) pairs, over a Lab PCS."""
    table = struct.pack(">I", len(tags))
    body = b""
    offset = 128 + 4 + 12 * len(tags)
    for sig, data in tags:
        table += sig + struct.pack(">II", offset + len(body), len(data))
        body += data + bytes(-len(data) % 4)
    header = bytearray(128)
    header[0:4] = struct.pack(">I", 128 + len(table) + len(body))
    header[8:12] = struct.pack(">I", version)
    header[12:16] = device_class
    header[16:20] = b"CMYK"
    header[20:24] = b"Lab "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + table + body


profile = icc_profile(
    ICC_VERSION_2_1,
    b"scnr",
    [
        (b"desc", text_description("litho CMYK, scanner class")),
        (b"cprt", b"text" + bytes(4) + b"No copyright\0"),
        (b"wtpt", b"XYZ " + bytes(4) + s15(*D50)),
        (b"A2B0", a2b0()),
        (b"B2A0", b2a0()),
    ],
)

destination = Stream(pdf, profile)
destination.N = 4
profile_ref = pdf.make_indirect(destination)
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name(subtype),
                OutputConditionIdentifier=String("litho CMYK"),
                DestOutputProfile=profile_ref,
            )
        )
        for subtype in ("/GTS_PDFA1", "/GTS_PDFX")
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
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            ColorSpace=Dictionary(CS0=Array([Name.ICCBased, profile_ref])),
        ),
        Contents=pdf.make_indirect(
            Stream(
                pdf,
                b"0.6 0.2 0 0.1 k 20 20 60 60 re f\n"
                b"/CS0 cs 0.1 0.5 0.9 0 scn 110 20 60 60 re f\n"
                b"0 0 0 1 k BT /F1 24 Tf 20 120 Td (AB) Tj ET\n",
            )
        ),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "iccbadcmyk_2b.pdf", min_version="1.7", fix_metadata_version=False)
