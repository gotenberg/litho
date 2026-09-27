#!/usr/bin/env python3
"""Build baretree_2b.pdf: a page tree short of the entries ISO 32000-1 requires
of it, which every rewrite has to write, at PDF/A-2b.

ISO 32000-1, 7.7.2, Table 28 makes the catalog's /Pages an indirect reference;
7.7.3.2, Table 29 makes a page tree node's /Type /Pages and a /Count of the
pages under it required; 7.7.3.3, Table 30 makes a page's /Parent, /Resources
and /MediaBox required, the last two inheritable, the box a rectangle (7.9.5).
A rewrite used to copy each of these as the source had it, and page insertion
and deletion refused a root with no object number. The corpus files that lack
/Type on the root or state a wrong /Count are damaged past what veraPDF parses
(pdfjs issue9105_other.pdf, Pages-tree-refs.pdf, the poppler fuzz files), and
none writes the root directly in the catalog.

The catalog writes the root directly, so no page can name it in /Parent. The
root has no /Type, lends no /MediaBox and no /Resources, and counts four of its
five pages:

| Page | Its own box              | Its own /Resources | Shows   |
| ---- | ------------------------ | ------------------ | ------- |
| 1    | 200 by 200               | the font           | `A`     |
| 2    | 200 by 200, by reference | the font           | `B`     |
| 3    | 200 by 200               | none               | nothing |
| 4    | 200 by 200               | `5`, no dictionary | nothing |
| 5    | none                     | the font           | `AB`    |

Page 2 is the control: its box is a rectangle written by reference, and stays
as it is. Pages 3 and 4 have no /Contents: veraPDF reports a page with no
/Resources whose content paints anything, DeviceGray included, under 6.2.2 t3
at PDF/A-4, which litho does not. The text is in the conforming embedded
TrueType of ttcmap_2b.pdf, in the initial DeviceGray a CMYK OutputIntent
covers, so no colour or font clause fires.

veraPDF 1.30.2 refuses a file holding a page tree node or a page with no
/Type, a root with no /Count or with a /Count larger than its pages, so those
shapes have no fixture. Two more are left out because veraPDF reads them in a
way litho does not, which would hide the constructs behind a divergence: with
no /Type on the catalog veraPDF reads no XMP metadata from it, and a /MediaBox
of three numbers it reads with the missing coordinate as 0, reporting 6.1.13
t11, which litho does not judge on a box short of four numbers. pdfjs
issue5334.pdf reaches the catalog's /Type in the corpus, and pdfjs
issue15150.pdf a /Resources naming no dictionary.

The tree is built by hand: pikepdf's page list flattens it, which would delete
the constructs the fixture exists for.

See ../README.md for the authoritative expected firing set.
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
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoBareSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)
BOX = Array([0, 0, 200, 200])


def page(ops, **entries):
    """A page naming no /Parent, since the root it sits under has no number.
    One with no ops is blank and has no /Contents."""
    if ops:
        entries["Contents"] = pdf.make_indirect(Stream(pdf, ops))
    return pdf.make_indirect(Dictionary(Type=Name.Page, **entries))


def text(word):
    return b"BT /F1 24 Tf 20 100 Td (" + word + b") Tj ET\n"


FONT = Dictionary(Font=Dictionary(F1=font))

pages = [
    page(text(b"A"), MediaBox=BOX, Resources=FONT),
    # ISO 32000-1, 7.3.10: any object may be indirect, the box included, and it
    # is still a rectangle.
    page(text(b"B"), MediaBox=pdf.make_indirect(Array([0, 0, 200, 200])), Resources=FONT),
    page(b"", MediaBox=BOX),
    page(b"", MediaBox=BOX, Resources=5),
    page(text(b"AB"), Resources=FONT),
]

# The root, written directly in the catalog, with no /Type and a /Count one
# short.
pdf.Root.Pages = Dictionary(Kids=Array(pages), Count=len(pages) - 1)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "baretree_2b.pdf", min_version="1.7")
