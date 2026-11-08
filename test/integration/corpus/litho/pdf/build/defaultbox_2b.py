#!/usr/bin/env python3
"""Build defaultbox_2b.pdf: an A4 document whose pages miss their media box, at
PDF/A-2b.

ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox required and inheritable, and
gives it no default. A viewer draws a page that neither states nor inherits a
box it uses on US Letter, and litho reads it so. A write that gives such a page
a media box gives it the size most pages of the document have instead, here
A4, 595.28 by 841.89, at origin 0 0: US Letter only when no page has a usable
box. No corpus file holds a page missing its media box among pages of another
size a majority shares but pdfjs boundingBox_invalid.pdf, whose sizes tie.

| Page | Its box, or its node's         | A viewer shows | A write gives it         |
| ---- | ------------------------------ | -------------- | ------------------------ |
| 1    | A4                             | A4             | A4                       |
| 2    | A4                             | A4             | A4                       |
| 3    | none                           | US Letter      | A4                       |
| 4    | US Letter                      | US Letter      | US Letter, the control   |
| 5    | none, /CropBox [0 0 590 830]   | 590 by 792     | A4, the crop box 590x830 |
| 6    | its node's /MediaBox [0 0 0 0] | US Letter      | A4, on the node          |
| 7    | A4                             | A4             | A4                       |

Page 5's crop box reaches past US Letter, which a viewer clips it to, and fits
A4, which the write clips it against. Page 6's node states a box enclosing no
area, which ISO 19005-2, 6.1.13 t11 fails and the PDF/A remediation repairs. Each
page draws its own word at (20, 100), inside every size, in the conforming
embedded TrueType of ttcmap_2b.pdf, in DeviceGray under a CMYK OutputIntent,
and states its own /Resources, so no colour, font or resource clause fires.

The tree is built by hand: pikepdf's page list flattens it, which would move
the node's box onto its page.

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
    FontName=Name("/LithoBoxSans"),
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
        BaseFont=Name("/LithoBoxSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)

A4 = [0, 0, 595.28, 841.89]
LETTER = [0, 0, 612, 792]

root = pdf.make_indirect(Dictionary(Type=Name.Pages))


def page(parent, word, **boxes):
    ops = b"BT /F1 24 Tf 20 100 Td (" + word + b") Tj ET\n"
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            Parent=parent,
            Contents=pdf.make_indirect(Stream(pdf, ops)),
            # A page's own, since ISO 19005-2, 6.2.2 t2 asks for resources
            # the content stream names explicitly.
            Resources=Dictionary(Font=Dictionary(F1=font)),
            **boxes,
        )
    )


node = pdf.make_indirect(
    Dictionary(Type=Name.Pages, Parent=root, MediaBox=Array([0, 0, 0, 0]))
)
page6 = page(node, b"BAB")
node.Kids = Array([page6])
node.Count = 1

root.Kids = Array(
    [
        page(root, b"A", MediaBox=Array(A4)),
        page(root, b"B", MediaBox=Array(A4)),
        page(root, b"AB"),
        page(root, b"BA", MediaBox=Array(LETTER)),
        page(root, b"AA", CropBox=Array([0, 0, 590, 830])),
        node,
        page(root, b"BB", MediaBox=Array(A4)),
    ]
)
root.Count = 7
pdf.Root.Pages = root

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "defaultbox_2b.pdf", min_version="1.7")
