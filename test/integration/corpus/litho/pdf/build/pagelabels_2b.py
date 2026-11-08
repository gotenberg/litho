#!/usr/bin/env python3
"""Build pagelabels_2b.pdf: page labels in every numbering style, under the
page edits, at PDF/A-2b.

ISO 32000-1, 12.4.2 makes /PageLabels a number tree keyed by page index, each
range giving its pages a style, a prefix and a start (Table 159), so an edit
that moves, removes or adds a page changes what every later page displays. A
split and a merge used to drop the tree, and page insertion and deletion kept
it as it was, keyed to pages that had moved. No litho fixture carried labels.

Eight pages, each showing its own text, carry one range per style:

| Page | Shows | Label   | Range                                                 |
| ---- | ----- | ------- | ----------------------------------------------------- |
| 1    | `A`   | `i`     | lowercase roman                                       |
| 2    | `B`   | `ii`    |                                                       |
| 3    | `AA`  | `Cover` | a prefix with no numbering style                      |
| 4    | `AB`  | `A-7`   | decimal, prefix `A-`, start 7, an object of its own   |
| 5    | `BA`  | `A-8`   |                                                       |
| 6    | `BB`  | `a`     | lowercase letters                                     |
| 7    | `AAA` | `b`     |                                                       |
| 8    | `AAB` | `Ü-IV`  | uppercase roman, a UTF-16BE prefix `Ü-`, start 4      |

The tree is a root with /Kids over two leaves, each carrying its /Limits (ISO
32000-1, 7.9.7, Table 37), and the range of page 4 is an indirect page label
dictionary with its optional /Type. mutate-pages moves page 1 to the end, and
each page has to keep its label there.

The file conforms to PDF/A-2b: the text is in the conforming embedded
TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
covers, as in baretree_2b.pdf, so no colour or font clause fires.

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
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoLabelSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoLabelSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)
FONT = Dictionary(Font=Dictionary(F1=font))

for word in (b"A", b"B", b"AA", b"AB", b"BA", b"BB", b"AAA", b"AAB"):
    content = Stream(pdf, b"BT /F1 24 Tf 20 100 Td (" + word + b") Tj ET\n")
    pdf.pages.append(
        pikepdf.Page(
            pdf.make_indirect(
                Dictionary(
                    Type=Name.Page,
                    MediaBox=Array([0, 0, 200, 200]),
                    Resources=FONT,
                    Contents=pdf.make_indirect(content),
                )
            )
        )
    )

# ISO 32000-1, 7.9.2.2: a text string may be UTF-16BE behind its byte order
# mark, here U+00DC and a hyphen.
annex_prefix = String(b"\xfe\xff\x00\xdc\x00\x2d")
decimal_range = pdf.make_indirect(
    Dictionary(Type=Name.PageLabel, S=Name.D, P=String("A-"), St=7)
)
first_leaf = pdf.make_indirect(
    Dictionary(
        Limits=Array([0, 3]),
        Nums=Array(
            [
                0,
                Dictionary(S=Name.r),
                2,
                Dictionary(P=String("Cover")),
                3,
                decimal_range,
            ]
        ),
    )
)
second_leaf = pdf.make_indirect(
    Dictionary(
        Limits=Array([5, 7]),
        Nums=Array([5, Dictionary(S=Name.a), 7, Dictionary(S=Name.R, P=annex_prefix, St=4)]),
    )
)
pdf.Root.PageLabels = pdf.make_indirect(Dictionary(Kids=Array([first_leaf, second_leaf])))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "pagelabels_2b.pdf", min_version="1.7")
