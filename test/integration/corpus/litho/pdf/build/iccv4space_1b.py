#!/usr/bin/env python3
"""Build iccv4space_1b.pdf: ICCBased colour spaces over an ICC v4 profile, at
PDF/A-1b.

PDF/A-1 rests on PDF 1.4, whose ICCBased colour spaces take ICC.1:1998-09 (v2)
profiles (PDF Reference 1.4, 4.5.4), so ISO 19005-1, 6.2.3.2 fails a v4 one,
and veraPDF's ICCInputProfile test reads "version < 3.0". PDF/A-2 and later
admit v4 (ISO 19005-2, 6.2.4.2). iccv4gray_1b.py puts the same profile in the
output intent; this one puts it where the content paints:

  - CS0, a grey ICCBased space over the v4.2 display profile, fills a square;
  - Im0, a 2x2 grey image, names the same profile stream in its own
    /ColorSpace, so the page and the image share one profile object;
  - CS1, the control, fills a second square in a grey ICCBased space over a
    v2.4 profile of the same transfer function, which no level fails.

The v4 profile has the shape of the v4.2 grey display profile pdfjs
issue19532.pdf carries: desc and cprt as multiLocalizedUnicodeType, the sRGB
transfer function as a type 3 parametricCurveType, and a D50 white point with
the Bradford adaptation from D65 in chad. None of those tags has its v4 type in
v2 (ICC.1:2001-04, 6.5), so the PDF/A-1 remediation must re-encode each one to
keep the colours. The file declares no output intent: ICCBased spaces need
none (ISO 19005-1, 6.2.3.3 judges device spaces alone).

Every profile value is computed here from published constants: the sRGB curve
parameters and D65 chromaticity of IEC 61966-2-1, and the Bradford cone
response matrix of ICC.1:2010, Annex E.3.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_VERSION_4_2 = 0x04200000
ICC_VERSION_2_4 = 0x02400000
D50 = (0.9642, 1.0, 0.8249)
D65 = (0.3127 / 0.3290, 1.0, (1 - 0.3127 - 0.3290) / 0.3290)
BRADFORD = (
    (0.8951, 0.2664, -0.1614),
    (-0.7502, 1.7135, 0.0367),
    (0.0389, -0.0685, 1.0296),
)
SRGB_CURVE = (2.4, 1 / 1.055, 0.055 / 1.055, 1 / 12.92, 0.04045)


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


def text_description(text):
    """A v2 textDescriptionType (ICC.1:2001-04, 6.5.17) of ASCII text."""
    ascii_text = text.encode("ascii") + b"\x00"
    return b"desc" + bytes(4) + struct.pack(">I", len(ascii_text)) + ascii_text + bytes(8) + bytes(2 + 1 + 67)


def srgb(x):
    """The sRGB transfer function of IEC 61966-2-1."""
    g, a, b, c, d = SRGB_CURVE
    return (a * x + b) ** g if x >= d else c * x


def icc_profile(version, tags):
    """An ICC grey display profile of tags, (signature, data) pairs."""
    table = struct.pack(">I", len(tags))
    body = b""
    offset = 128 + 4 + 12 * len(tags)
    for sig, data in tags:
        table += sig + struct.pack(">II", offset + len(body), len(data))
        body += data + bytes(-len(data) % 4)
    header = bytearray(128)
    header[0:4] = struct.pack(">I", 128 + len(table) + len(body))
    header[8:12] = struct.pack(">I", version)
    header[12:16] = b"mntr"
    header[16:20] = b"GRAY"
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + table + body


chad = bradford(D65, D50)
v4_profile = icc_profile(
    ICC_VERSION_4_2,
    [
        (b"desc", mluc("litho sRGB grey v4")),
        (b"cprt", mluc("No copyright")),
        (b"wtpt", b"XYZ " + bytes(4) + s15(*D50)),
        (b"chad", b"sf32" + bytes(4) + s15(*(v for row in chad for v in row))),
        (b"kTRC", b"para" + bytes(4) + struct.pack(">HH", 3, 0) + s15(*SRGB_CURVE)),
    ],
)
SAMPLES = 1024
v2_profile = icc_profile(
    ICC_VERSION_2_4,
    [
        (b"desc", text_description("litho sRGB grey v2")),
        (b"cprt", b"text" + bytes(4) + b"No copyright\x00"),
        (b"wtpt", b"XYZ " + bytes(4) + s15(*D65)),
        (
            b"kTRC",
            b"curv" + bytes(4) + struct.pack(">I", SAMPLES)
            + b"".join(struct.pack(">H", round(srgb(i / (SAMPLES - 1)) * 65535)) for i in range(SAMPLES)),
        ),
    ],
)


def iccbased(profile):
    stream = Stream(pdf, profile)
    stream.N = 1
    return pdf.make_indirect(stream)


v4 = iccbased(v4_profile)
v2 = iccbased(v2_profile)

image = pdf.make_indirect(
    Stream(
        pdf,
        bytes([0x00, 0x55, 0xAA, 0xFF]),
        Type=Name.XObject,
        Subtype=Name.Image,
        Width=2,
        Height=2,
        BitsPerComponent=8,
        ColorSpace=Array([Name.ICCBased, v4]),
    )
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

content = (
    b"/CS0 cs 0.5 sc 20 20 40 40 re f\n"
    b"/CS1 cs 0.5 sc 140 20 40 40 re f\n"
    b"q 40 0 0 40 80 20 cm /Im0 Do Q\n"
    b"/CS0 cs 0 sc BT /F1 24 Tf 20 120 Td (AB) Tj ET\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(
            ColorSpace=Dictionary(CS0=Array([Name.ICCBased, v4]), CS1=Array([Name.ICCBased, v2])),
            XObject=Dictionary(Im0=image),
            Font=Dictionary(F1=font),
        ),
        Contents=pdf.make_indirect(Stream(pdf, content)),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))
save(pdf, "iccv4space_1b.pdf", min_version="1.4", fix_metadata_version=False)
