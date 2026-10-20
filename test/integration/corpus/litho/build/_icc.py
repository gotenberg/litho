"""Version 2.1 ICC destination profiles for the output-intent fixtures.

Every part of PDF/A admits a version 2 profile of the printer or monitor
device class as an output intent's destination profile (ISO 19005-1, 6.2.2;
ISO 19005-2/3/4, 6.2.3 t1). The profiles carry real transforms: the RGB one a
matrix and gamma 2.2 curves over an XYZ connection space, the CMYK one lut8
A2B0 and B2A0 tables over Lab. They are the profiles twoprofile_1b.pdf
carries, written by the same code.
"""

import struct

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


COMMON_TAGS = [
    (b"cprt", b"text" + bytes(4) + b"No copyright\0"),
    (b"wtpt", xyz(D50)),
]


def rgb_display_profile():
    """A version 2.1 RGB display profile."""
    return icc_profile(
        b"mntr",
        b"RGB ",
        b"XYZ ",
        [(b"desc", text_description("litho RGB display"))]
        + COMMON_TAGS
        + [(sig, xyz(v)) for sig, v in PRIMARIES.items()]
        + [(sig, gamma_curve(2.2)) for sig in (b"rTRC", b"gTRC", b"bTRC")],
    )


def cmyk_printer_profile():
    """A version 2.1 CMYK printer profile."""
    return icc_profile(
        b"prtr",
        b"CMYK",
        b"Lab ",
        [(b"desc", text_description("litho CMYK printer"))] + COMMON_TAGS + [(b"A2B0", a2b0()), (b"B2A0", b2a0())],
    )
