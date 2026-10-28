#!/usr/bin/env python3
"""Build iccv4gray_1b.pdf: a grey output intent whose ICC v4 profile carries
the tags a colour engine reads, at PDF/A-1b.

PDF/A-1 rests on PDF 1.4, which predates ICC v4, so ISO 19005-1, 6.2.2 takes
the destination profile of PDF Reference 1.4, 9.10.4, an ICC.1:1998-09 (v2)
profile, and veraPDF's test reads "version < 3.0". iccv4intent_1b.py plants a
v4 profile with no tags at all; this one carries the shape of the v4.2 grey
display profile pdfjs issue19532.pdf has in its intent, whose every tag has a
version 2 form only through another type:

  - desc and cprt are multiLocalizedUnicodeType (ICC.1:2010, 10.15), which
    version 2 writes as textDescriptionType and textType;
  - kTRC is a parametricCurveType of function type 3 (10.18), the sRGB
    transfer function of IEC 61966-2-1, which version 2 can only sample;
  - wtpt is the D50 illuminant, as ICC.1:2010, 9.2.36 has a v4 display
    profile state it, with chad the Bradford adaptation from D65 (9.2.15),
    where a v2 display profile records the display's own white.

Remediation re-encodes it as version 2 with the same colour transforms. No
file of the litho or veraPDF corpora reaches that conversion; the pdfjs file
that does is not remediated by the gate.

Every profile value is computed here from published constants: the sRGB
curve parameters and D65 chromaticity of IEC 61966-2-1, and the Bradford cone
response matrix of ICC.1:2010, Annex E.3.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_VERSION_4_2 = 0x04200000
D50 = (0.9642, 1.0, 0.8249)
D65 = (0.3127 / 0.3290, 1.0, (1 - 0.3127 - 0.3290) / 0.3290)
BRADFORD = (
    (0.8951, 0.2664, -0.1614),
    (-0.7502, 1.7135, 0.0367),
    (0.0389, -0.0685, 1.0296),
)


def mat_vec(m, v):
    return tuple(sum(m[i][j] * v[j] for j in range(3)) for i in range(3))


def mat_mul(a, b):
    return tuple(tuple(sum(a[i][k] * b[k][j] for k in range(3)) for j in range(3)) for i in range(3))


def mat_inv(m):
    (a, b, c), (d, e, f), (g, h, i) = m
    det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g)
    return (
        ((e * i - f * h) / det, (c * h - b * i) / det, (b * f - c * e) / det),
        ((f * g - d * i) / det, (a * i - c * g) / det, (c * d - a * f) / det),
        ((d * h - e * g) / det, (b * g - a * h) / det, (a * e - b * d) / det),
    )


def bradford(src, dst):
    """The Bradford adaptation from white src to white dst (ICC.1:2010, E.3)."""
    s, d = mat_vec(BRADFORD, src), mat_vec(BRADFORD, dst)
    scale = tuple(tuple(d[i] / s[i] if i == j else 0.0 for j in range(3)) for i in range(3))
    return mat_mul(mat_inv(BRADFORD), mat_mul(scale, BRADFORD))


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def mluc(text):
    units = text.encode("utf-16-be")
    return b"mluc" + bytes(4) + struct.pack(">II", 1, 12) + b"enUS" + struct.pack(">II", len(units), 28) + units


def icc_profile(tags):
    """An ICC v4.2 grey display profile of tags, (signature, data) pairs."""
    table = struct.pack(">I", len(tags))
    body = b""
    offset = 128 + 4 + 12 * len(tags)
    for sig, data in tags:
        table += sig + struct.pack(">II", offset + len(body), len(data))
        body += data + bytes(-len(data) % 4)
    header = bytearray(128)
    header[0:4] = struct.pack(">I", 128 + len(table) + len(body))
    header[8:12] = struct.pack(">I", ICC_VERSION_4_2)
    header[12:16] = b"mntr"
    header[16:20] = b"GRAY"
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + table + body


chad = bradford(D65, D50)
profile = icc_profile(
    [
        (b"desc", mluc("litho sRGB grey v4")),
        (b"cprt", mluc("No copyright")),
        (b"wtpt", b"XYZ " + bytes(4) + s15(*D50)),
        (b"chad", b"sf32" + bytes(4) + s15(*(v for row in chad for v in row))),
        (b"kTRC", b"para" + bytes(4) + struct.pack(">HH", 3, 0) + s15(2.4, 1 / 1.055, 0.055 / 1.055, 1 / 12.92, 0.04045)),
    ]
)

destination = Stream(pdf, profile)
destination.N = 1
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("sGray"),
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
save(pdf, "iccv4gray_1b.pdf", min_version="1.4", fix_metadata_version=False)
