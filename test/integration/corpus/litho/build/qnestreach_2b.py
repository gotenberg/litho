#!/usr/bin/env python3
"""Build qnestreach_2b.pdf: q/Q nesting past the limit in streams a page draws.

ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 cap q/Q nesting at 28 levels
(ISO 32000-1, Annex C, Table C.1). veraPDF 1.30.2 counts the levels of every
content stream it parses on their own, starting at zero, and fails the test
once for each q past the 28th: in the page's content, and in each form
XObject, tiling pattern cell, Type 3 glyph description and annotation
appearance the page draws. No corpus file nests that deep outside a page's own
content, so this fixture plants one stream per page, each at its own depth so
the count names the stream:

| Page | Stream                                  | Levels | Answers at PDF/A-2b |
| ---- | --------------------------------------- | ------ | ------------------- |
| 1    | a form XObject                          | 29     | `6.1.13/8` once     |
| 2    | a tiling pattern cell                   | 30     | twice               |
| 3    | a Type 3 glyph description              | 31     | three times         |
| 4    | an annotation's normal appearance       | 32     | four times          |
| 5    | page 1's form, painted again            | 29     | nothing             |
| 6    | a page 20 deep painting a form 15 deep  | 20, 15 | nothing             |

Pages 5 and 6 are must-not-fire controls: a stream is judged once however
often it is drawn, and a form's levels do not add to its painter's.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as nestedpattern_2b.py builds it."""
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


def nested(levels):
    """A grey square painted inside levels of q/Q."""
    return b"q " * levels + b"0 g 0 0 10 10 re f " + b"Q " * levels + b"\n"


def form(content):
    """A form XObject with empty /Resources."""
    xobj = Stream(pdf, content)
    xobj.Type = Name.XObject
    xobj.Subtype = Name.Form
    xobj.BBox = Array([0, 0, 100, 100])
    xobj.Resources = Dictionary()
    return pdf.make_indirect(xobj)


def add_page(content, resources, annots=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=resources,
        Contents=pdf.make_indirect(Stream(pdf, content)),
    )
    if annots is not None:
        page.Annots = annots
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


form1 = form(nested(29))
add_page(b"q /X1 Do Q\n", Dictionary(XObject=Dictionary(X1=form1)))

cell = Stream(pdf, nested(30))
cell.Type = Name.Pattern
cell.PatternType = 1
cell.PaintType = 1
cell.TilingType = 1
cell.BBox = Array([0, 0, 20, 20])
cell.XStep = 20
cell.YStep = 20
cell.Resources = Dictionary()
add_page(
    b"q /Pattern cs /P2 scn 0 0 100 100 re f Q\n",
    Dictionary(Pattern=Dictionary(P2=pdf.make_indirect(cell))),
)

glyph = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n" + nested(31)))
type3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(A=glyph),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A])),
        FirstChar=65,
        LastChar=65,
        Widths=Array([1000]),
        Resources=Dictionary(),
    )
)
add_page(b"q 0 g BT /T3 50 Tf 10 10 Td (A) Tj ET Q\n", Dictionary(Font=Dictionary(T3=type3)))

annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([0, 0, 100, 100]),
        F=4,
        AP=Dictionary(N=form(nested(32))),
    )
)
add_page(b"q 0 g 0 0 1 1 re f Q\n", Dictionary(), Array([annot]))

add_page(b"q /X1 Do Q\n", Dictionary(XObject=Dictionary(X1=form1)))

add_page(
    b"q " * 20 + b"/X6 Do " + b"Q " * 20 + b"\n",
    Dictionary(XObject=Dictionary(X6=form(nested(15)))),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "qnestreach_2b.pdf")
