#!/usr/bin/env python3
"""Build iccbased_4.pdf: ICCBased colour spaces under PDF/A-4.

The coverage scan showed ISO 19005-4, 6.2.4.2 unreached. It judges the ICC
profile an ICCBased colour space carries, and the two ways such a space may
conflict with the file's destination profile.

Four ICCBased spaces are drawn on one page, three defective and one
conforming, which is the masking probe: a check that reports the first
offending colour space and stops reports one rule where three are due.

| Space | Construct                                              | Rule        |
| ----- | ------------------------------------------------------ | ----------- |
| CS0   | profile version 5.0                                    | `6.2.4.2/1` |
| CS1   | CMYK, stroked with `/OP true` and `/OPM 1`             | `6.2.4.2/2` |
| CS2   | the OutputIntent's own destination profile             | `6.2.4.2/3` |
| CS3   | RGB, `mntr`, version 2.1, unrelated to the OutputIntent | silent     |

The three defects are kept apart deliberately, because two of them are easy to
trigger by accident. Test 3 compares the ICCBased profile against the
OutputIntent's by object and by MD5, so any CMYK space carrying the same bytes
fires it: CS1 therefore uses a profile that differs from the destination one,
or it would report test 3 as well and the overprint verdict would be masked.
CS3 is RGB rather than CMYK, which keeps it outside test 3 entirely.

The profiles are built here rather than shipped. These tests read the header,
so each is a 128-byte header plus an empty tag table, carrying a real size, an
`acsp` signature and a PCS illuminant, because veraPDF parses profile bytes
eagerly and throws on a placeholder.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000
ICC_VERSION_5_0 = 0x05000000


def icc_profile(device_class, colour_space, version=ICC_VERSION_2_1, tag=b"lith"):
    """A structurally minimal ICC profile with the chosen header fields.

    `tag` is the preferred CMM field. It carries no meaning for these tests and
    exists only to make otherwise identical profiles differ byte for byte,
    which is what keeps test 3 off the spaces that are not about it.
    """
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


def profile_stream(data, components):
    stream = Stream(pdf, data)
    stream.N = components
    return pdf.make_indirect(stream)


def icc_based(profile):
    return pdf.make_indirect(Array([Name.ICCBased, profile]))


font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# The destination profile. Test 3 measures the other spaces against this one.
destination = profile_stream(icc_profile(b"prtr", b"CMYK", tag=b"OUTI"), 4)
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=destination,
            )
        )
    ]
)

colour_spaces = Dictionary(
    # Version 5.0, which the clause caps below (t1).
    CS0=icc_based(profile_stream(icc_profile(b"prtr", b"CMYK", ICC_VERSION_5_0, b"V5PR"), 4)),
    # A valid CMYK profile, distinct from the destination, stroked under an
    # overprint mode of 1 (t2).
    CS1=icc_based(profile_stream(icc_profile(b"mntr", b"CMYK", tag=b"ALT1"), 4)),
    # The destination profile itself (t3).
    CS2=icc_based(destination),
    # Conforming and unrelated: RGB, so test 3 does not reach it.
    CS3=icc_based(profile_stream(icc_profile(b"mntr", b"RGB ", tag=b"RGBP"), 3)),
)

overprint = pdf.make_indirect(
    Dictionary(Type=Name.ExtGState, OP=True, op=True, OPM=1)
)

content = (
    b"BT /F1 12 Tf 50 740 Td (ICCBased colour spaces) Tj ET\n"
    b"q /CS0 cs 0.1 0.2 0.3 0.4 scn 50 700 80 30 re f Q\n"
    # Only CS1 is stroked under the overprint state, so the mode-1 verdict
    # belongs to it alone.
    b"q /GS0 gs /CS1 CS 0.1 0.2 0.3 0.4 SCN 2 w 50 660 80 30 re S Q\n"
    b"q /CS2 cs 0.1 0.2 0.3 0.4 scn 50 620 80 30 re f Q\n"
    b"q /CS3 cs 0.2 0.4 0.6 scn 50 580 80 30 re f Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            ColorSpace=colour_spaces,
            ExtGState=Dictionary(GS0=overprint),
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata.
save(pdf, "iccbased_4.pdf", min_version="2.0")
