#!/usr/bin/env python3
"""Build twoprofile_1b.pdf: a PDF/X output intent listed before the PDF/A one,
each naming its own destination profile of another colour space, at PDF/A-1b.

ISO 19005-1, 6.2.2 asks every output intent carrying /DestOutputProfile to
name the same indirect object, and ISO 19005-2/3/4, 6.2.3 t2 ask the same, so
every level fails this file: its GTS_PDFX intent names a CMYK printer profile
and its GTS_PDFA1 intent an RGB display profile.

The order is the probe. The PDF/A intent is the one PDF/A reads device colour
through (ISO 19005-1, 6.2.3.3), and the page paints DeviceRGB, which only an
RGB PDF/A intent covers. A repair that unifies on the first entry gives the
PDF/A intent the CMYK profile and leaves the DeviceRGB square uncovered; one
that keeps the PDF/A intent's profile and points the PDF/X intent at it leaves
the page conforming. Both intents are well formed, so the shared-object rule is
the only output-intent rule the file fails.

Both profiles are version 2.1, which PDF/A-1 asks of a destination profile,
and carry real transforms: the RGB one a matrix and gamma 2.2 curves over an
XYZ connection space, the CMYK one lut8 A2B0 and B2A0 tables over Lab, as in
iccbadcmyk_1b.pdf. The page carries no text, so no font rule fires.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_VERSION_2_1 = 0x02100000
D50 = (0.9642, 1.0, 0.8249)
IDENTITY = (1, 0, 0, 0, 1, 0, 0, 0, 1)
# The sRGB primaries adapted to D50, the columns of its RGB-to-XYZ matrix.
PRIMARIES = {
    b"rXYZ": (0.4361, 0.2225, 0.0139),
    b"gXYZ": (0.3851, 0.7169, 0.0971),
    b"bXYZ": (0.1431, 0.0606, 0.7141),
}


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def text_description(text):
    """A v2 textDescriptionType (ICC.1:2001-04, 6.5.17): ASCII only."""
    ascii_text = text.encode("ascii") + b"\0"
    return b"desc" + bytes(4) + struct.pack(">I", len(ascii_text)) + ascii_text + bytes(4 + 4 + 2 + 1 + 67)


def xyz(values):
    return b"XYZ " + bytes(4) + s15(*values)


def gamma_curve(gamma):
    """A curveType (ICC.1:2001-04, 6.5.3) of one entry: a u8Fixed8 gamma."""
    return b"curv" + bytes(4) + struct.pack(">IH", 1, round(gamma * 256))


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


def icc_profile(device_class, colour_space, pcs, tags):
    """A version 2.1 ICC profile of tags, (signature, data) pairs."""
    table = struct.pack(">I", len(tags))
    body = b""
    offset = 128 + 4 + 12 * len(tags)
    for sig, data in tags:
        table += sig + struct.pack(">II", offset + len(body), len(data))
        body += data + bytes(-len(data) % 4)
    header = bytearray(128)
    header[0:4] = struct.pack(">I", 128 + len(table) + len(body))
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = pcs
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + table + body


def destination(profile, components):
    stream = Stream(pdf, profile)
    stream.N = components
    return pdf.make_indirect(stream)


common_tags = [
    (b"cprt", b"text" + bytes(4) + b"No copyright\0"),
    (b"wtpt", xyz(D50)),
]
rgb = icc_profile(
    b"mntr",
    b"RGB ",
    b"XYZ ",
    [(b"desc", text_description("litho RGB display"))]
    + common_tags
    + [(sig, xyz(v)) for sig, v in PRIMARIES.items()]
    + [(sig, gamma_curve(2.2)) for sig in (b"rTRC", b"gTRC", b"bTRC")],
)
cmyk = icc_profile(
    b"prtr",
    b"CMYK",
    b"Lab ",
    [(b"desc", text_description("litho CMYK printer"))] + common_tags + [(b"A2B0", a2b0()), (b"B2A0", b2a0())],
)

pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFX,
                OutputConditionIdentifier=String("litho CMYK printer"),
                DestOutputProfile=destination(cmyk, 4),
            )
        ),
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho RGB display"),
                DestOutputProfile=destination(rgb, 3),
            )
        ),
    ]
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"0.8 0.1 0.1 rg 20 20 60 60 re f\n")),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))
save(pdf, "twoprofile_1b.pdf", min_version="1.4", fix_metadata_version=False)
