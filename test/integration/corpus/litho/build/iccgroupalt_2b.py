#!/usr/bin/env python3
"""Build iccgroupalt_2b.pdf: transparency groups blending in, and transparent
text drawn in, ICCBased spaces whose profiles no colour engine can use, each
with an /Alternate of another family than its /N names, at PDF/A-2b.

ISO 32000-1, 8.6.5.5, Table 66 has a reader that cannot use an ICCBased
space's profile use the stream's /Alternate, with the colour values as they
are. A transparency group blends in the space its /CS names (11.3.4, Table
147), which admits no special space, and flattening composites transparent
text over white paper in the text's own space (11.3.7.2), where the paper is
the alternate's. No corpus file reached either: the only unusable profiles
in a group's /CS or under transparent text name no /Alternate, or the device
space of /N.

Each profile is a version 2.1 header with an empty tag table, as
iccalternate_2b.pdf's are: veraPDF parses it, but it holds no transform, so
every reader falls back to its /Alternate. Every glyph is a Type 3 glyph
painting in the text's colour, with a /ToUnicode map:

| Page | Draws                                                                    |
| ---- | ------------------------------------------------------------------------ |
| 1    | an isolated group whose /CS has a CMYK header and /Alternate DeviceCMYK, |
|      | blue at ca 0.6 over yellow, and an isolated group whose /CS has a GRAY   |
|      | header and a Separation /Alternate, red then green Multiply              |
| 2    | text at ca 0.5 in the GRAY-header space, tint 0.6, over the bare page    |

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, tounicode_cmap, type3_font, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000
D50 = (0.9642, 1.0, 0.8249)


def s15(*values):
    return b"".join(struct.pack(">i", round(v * 65536)) for v in values)


def header_only_profile(colour_space):
    """A version 2.1 monitor profile of the colour space with no tags."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = s15(*D50)
    return bytes(header) + tag_table


def iccbased(colour_space, n, alternate):
    profile = Stream(pdf, header_only_profile(colour_space))
    profile.N = n
    profile.Alternate = alternate
    return Array([Name.ICCBased, pdf.make_indirect(profile)])


magenta_tint = Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([0, 1, 0, 0]), N=1)
cmyk_like = iccbased(b"CMYK", 4, Name.DeviceCMYK)
spot_like = iccbased(b"GRAY", 1, Array([Name.Separation, Name.Spot, Name.DeviceCMYK, magenta_tint]))

GLYPHS = [
    ("A", 600, "0 0 500 700 re f"),
    ("B", 600, "0 0 500 700 re 100 100 300 500 re f*"),
    ("C", 600, "0 0 m 500 0 l 250 700 l f"),
]
font = type3_font(pdf, GLYPHS, [600] * len(GLYPHS))
font.ToUnicode = tounicode_cmap(pdf, {65 + i: ord(name) for i, (name, _, _) in enumerate(GLYPHS)})
font = pdf.make_indirect(font)


def form(content, bbox, group, resources):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array(bbox)
    stream.Group = group
    stream.Resources = resources
    return pdf.make_indirect(stream)


def transparency(**attrs):
    return Dictionary(Type=Name.Group, S=Name.Transparency, **attrs)


def state(**entries):
    return pdf.make_indirect(Dictionary(Type=Name.ExtGState, **entries))


def add_page(content, resources):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 100]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


over_yellow = form(
    b"0 0 1 0 k 0 0 80 80 re f /H gs 0 0 1 rg 20 20 60 60 re f\n",
    [0, 0, 80, 80],
    transparency(I=True, CS=cmyk_like),
    Dictionary(ExtGState=Dictionary(H=state(ca=0.6))),
)
multiply = form(
    b"1 0 0 rg 0 0 60 80 re f /M gs 0 1 0 rg 20 0 60 80 re f\n",
    [0, 0, 80, 80],
    transparency(I=True, CS=spot_like),
    Dictionary(ExtGState=Dictionary(M=state(BM=Name.Multiply))),
)
add_page(
    b"q 1 0 0 1 10 10 cm /Y Do Q q 1 0 0 1 110 10 cm /M Do Q\n",
    Dictionary(XObject=Dictionary(Y=over_yellow, M=multiply)),
)
add_page(
    b"q /Half gs /S cs 0.6 sc BT /F 30 Tf 10 40 Td (ABCABC) Tj ET Q\n",
    Dictionary(
        Font=Dictionary(F=font),
        ColorSpace=Dictionary(S=spot_like),
        ExtGState=Dictionary(Half=state(ca=0.5, CA=0.5)),
    ),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "iccgroupalt_2b.pdf", min_version="1.7")
