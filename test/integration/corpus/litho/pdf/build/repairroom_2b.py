#!/usr/bin/env python3
"""Build repairroom_2b.pdf: repairs that keep what a text operator moves and
reach the colour space of a shading.

The `.notdef` repair strips a code a font gives no glyph from the strings a
page shows. Two things the stripped operation did besides showing a glyph are
planted here, each with a visible string after it on the same line. No corpus
page the repair runs on carries the first; the second is on 18 corpus files,
litho repairleft_4.pdf and inlineimage_2b.pdf among them:

| Line | Operations                          | What stripping `\\x01` must keep       |
| ---- | ----------------------------------- | -------------------------------------- |
| 1    | `(AB) Tj [(\\x01) -3000] TJ (BA) Tj` | the 3000-unit move of the emptied TJ   |
| 2    | `(A\\x01B) Tj (AB) Tj`               | the 600-unit width of the `.notdef`    |

A number in a TJ array moves the text position (ISO 32000-1, 9.4.3), and a
glyph moves it by its width (9.4.4). The repair dropped an emptied TJ whole
and removed the glyph with its width, so poppler drew `BA` 43.2 units and the
second `B` and `AB` 7.2 units further left than the source.

The page also paints with two DeviceN colour spaces over spot colorants with
no `/Colorants`, which ISO 19005-2, 6.2.4.4 t1 fails, each in a shading
rather than the page's `/ColorSpace` resources:

| Name  | Where                                         | Colorants          |
| ----- | --------------------------------------------- | ------------------ |
| `P0`  | a shading pattern `scn` paints with           | SpotX and SpotY    |
| `Sh0` | a shading `sh` paints, in `/Shading`          | SpotX and SpotZ    |

`CSX`, a Separation of SpotX the page also paints with, is the definition a
completed `/Colorants` must reuse (6.2.4.4 t2). The `/Colorants` repair walked
the page's `/ColorSpace` resources alone, so both kept t1. Of the five corpus
files whose shadings use a DeviceN short of colorants, pdfjs issue17065.pdf is
the one a remediation left short, and the remediation corpus runs none of
them.

The font is the conforming non-symbolic TrueType of `repairleft_4.pdf`,
embedded, with `/Widths` from code 1 so the `.notdef` width agrees with the
dictionary. A CMYK OutputIntent covers the DeviceCMYK alternates.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

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

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
FIRST, LAST = 1, 66
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRepairSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRepairSans"),
                Flags=32,  # non-symbolic, ISO 32000-1, Table 123
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile2=pdf.make_indirect(font_file),
            )
        ),
        Encoding=Name.WinAnsiEncoding,
    )
)


def spot_tint(c1):
    """A Type 2 tint transform from one spot component to CMYK."""
    return pdf.make_indirect(
        Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array(c1), N=1)
    )


def devicen(second):
    """A DeviceN over SpotX and second, with no /Attributes."""
    tint = Stream(pdf, b"{ pop pop 0 0 0 1 }")
    tint.FunctionType = 4
    tint.Domain = Array([0, 1, 0, 1])
    tint.Range = Array([0, 1, 0, 1, 0, 1, 0, 1])
    return Array([Name.DeviceN, Array([Name("/SpotX"), Name(second)]), Name.DeviceCMYK, pdf.make_indirect(tint)])


def axial(colour_space):
    """An axial shading over colour_space, both components 0 to 1."""
    return Dictionary(
        ShadingType=2,
        ColorSpace=colour_space,
        Coords=Array([0, 0, 100, 0]),
        Function=pdf.make_indirect(
            Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0]), C1=Array([1, 1]), N=1)
        ),
    )


pattern = pdf.make_indirect(Dictionary(Type=Name.Pattern, PatternType=2, Shading=axial(devicen("/SpotY"))))

content = (
    b"BT /F1 12 Tf 72 700 Td (AB) Tj [(\x01) -3000] TJ (BA) Tj ET\n"
    b"BT /F1 12 Tf 72 670 Td (A\x01B) Tj (AB) Tj ET\n"
    b"q /CSX cs 1 scn 50 500 40 40 re f Q\n"
    b"q /Pattern cs /P0 scn 100 500 100 40 re f Q\n"
    b"q 250 500 100 40 re W n 1 0 0 1 250 500 cm /Sh0 sh Q\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            ColorSpace=Dictionary(CSX=Array([Name.Separation, Name("/SpotX"), Name.DeviceCMYK, spot_tint([0, 0, 1, 0])])),
            Pattern=Dictionary(P0=pattern),
            Shading=Dictionary(Sh0=axial(devicen("/SpotZ"))),
        ),
        Contents=pdf.make_indirect(Stream(pdf, content)),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "repairroom_2b.pdf", fix_metadata_version=False)
