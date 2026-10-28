#!/usr/bin/env python3
"""Build iccbadgray_2b.pdf: a direct grey output intent whose ICC profile
is a scanner profile, at PDF/A-2b.

ISO 19005-2, 6.2.3 asks the destination profile of a PDF/A output intent to
be an output (prtr) or display (mntr) profile, and ISO 19005-1, 6.2.2 and
19005-4, 6.2.3 ask the same, so every level fails this intent, whose header
declares the input device class scnr. outputintent_2b.pdf fails an RGB intent
the same way.

Remediation replaces the profile with a stock profile of the same data colour
space. The header names GRAY, so the replacement is litho's generated v2.4
sRGB grey profile (sgray-v2.icc), not sRGB: an RGB destination under a grey
output condition would license DeviceRGB content the source never had (ISO
19005-2, 6.2.4.3).

An ICCBased space admits a scanner profile (ISO 19005-2, 6.2.4.2), so
remediation keeps the source profile for any space sharing it and points the
intent at a new profile object. The intent sits directly in /OutputIntents,
with no object number of its own (ISO 32000-1, 7.3.10), so remediation
rewrites the catalog's array. It used to overwrite the profile stream at its
own object number instead, after allocating the replacement once more under a
number nothing referenced, so every output carried an orphan copy of the
profile. iccbadgray_1b.pdf takes the in-place branch.

The profile carries the version 2.1 grey tags a colour engine reads: desc as
textDescriptionType, cprt as textType, the D65 white of IEC 61966-2-1 in wtpt,
and a gamma 2.2 kTRC curve. Only the device class is wrong. The page paints a
DeviceGray square and its text in the conforming embedded TrueType of
baretree_2b.pdf, both in DeviceGray, which the grey intent covers.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_VERSION_2_1 = 0x02100000
D50 = (0.9642, 1.0, 0.8249)
D65 = (0.3127 / 0.3290, 1.0, (1 - 0.3127 - 0.3290) / 0.3290)


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def text_description(text):
    """A v2 textDescriptionType (ICC.1:2001-04, 6.5.17): ASCII only."""
    ascii_text = text.encode("ascii") + b"\0"
    return b"desc" + bytes(4) + struct.pack(">I", len(ascii_text)) + ascii_text + bytes(4 + 4 + 2 + 1 + 67)


def icc_profile(version, device_class, tags):
    """A grey ICC profile of tags, (signature, data) pairs."""
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
    header[16:20] = b"GRAY"
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + table + body


profile = icc_profile(
    ICC_VERSION_2_1,
    b"scnr",
    [
        (b"desc", text_description("litho grey, scanner class")),
        (b"cprt", b"text" + bytes(4) + b"No copyright\0"),
        (b"wtpt", b"XYZ " + bytes(4) + s15(*D65)),
        (b"kTRC", b"curv" + bytes(4) + struct.pack(">IH", 1, 0x0233)),  # gamma 2.2 in u8Fixed8
    ],
)

destination = Stream(pdf, profile)
destination.N = 1
pdf.Root.OutputIntents = Array(
    [
        Dictionary(
            Type=Name.OutputIntent,
            S=Name.GTS_PDFA1,
            OutputConditionIdentifier=String("litho grey"),
            DestOutputProfile=pdf.make_indirect(destination),
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
        Contents=pdf.make_indirect(
            Stream(pdf, b"0.5 g 20 20 60 60 re f\n0 g BT /F1 24 Tf 20 120 Td (AB) Tj ET\n")
        ),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "iccbadgray_2b.pdf", min_version="1.7", fix_metadata_version=False)
