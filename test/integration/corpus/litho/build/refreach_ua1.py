#!/usr/bin/env python3
"""Build refreach_ua1.pdf: reference XObjects a pattern, a glyph or a page draws.

ISO 14289-1, 7.20 forbids reference XObjects: a form XObject whose dictionary
carries /Ref (ISO 32000-1, 8.10.4, Table 95). veraPDF 1.30.2 states the test on
the form object a Do builds, as it states ISO 19005-2, 6.2.9 t2, so it judges a
form at every Do that draws it, in every content stream it parses: the cell of
a tiling pattern an scn selects, the glyph of a Type 3 font text is shown with,
an annotation's appearance. A form a /Resources only lists is never judged. No
corpus file draws a reference XObject at PDF/UA-1, so this fixture plants one
path per page, each with a reference form of its own:

| Page | Path                                             | Answers at PDF/UA-1 |
| ---- | ------------------------------------------------ | ------------------- |
| 1    | tiling cell, `/R Do`                             | `7.20/1` once       |
| 2    | Type 3 glyph the page shows, `/R Do`             | `7.20/1` once       |
| 3    | tiling cell, Type 3 glyph, `/R Do`               | `7.20/1` once       |
| 4    | the page's content, `/R Do /R Do`                | `7.20/1` twice      |
| 5    | annotation appearance, `/R Do`                   | `7.20/1` once       |
| 6    | the page, a cell and a Type 3 font list R, no Do | nothing             |

Page 6 is the must-not-fire control: its page resources, the cell of the
pattern it selects, and the Type 3 font it shows text in each list a reference
form that nothing draws, and the glyph the page shows draws none.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as patternglyph_2b.py builds it."""
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


def form(content, resources=None, ref=False):
    """A form XObject; ref makes it a reference XObject."""
    xobj = Stream(pdf, content)
    xobj.Type = Name.XObject
    xobj.Subtype = Name.Form
    xobj.BBox = Array([0, 0, 20, 20])
    if resources is not None:
        xobj.Resources = resources
    if ref:
        xobj.Ref = Dictionary(F=String("target.pdf"), Page=0)
    return pdf.make_indirect(xobj)


def reference():
    """A reference XObject whose own content paints a square."""
    return form(b"0 g 0 0 10 10 re f\n", Dictionary(), ref=True)


def tiling(content, resources):
    """A coloured tiling pattern carrying its own /Resources."""
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = 1
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 20, 20])
    pat.XStep = 20
    pat.YStep = 20
    pat.Resources = resources
    return pdf.make_indirect(pat)


def type3(glyph, resources):
    """A one-glyph Type 3 font; the glyph sets its colour (d0)."""
    proc = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n" + glyph))
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(A=proc),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A])),
            FirstChar=65,
            LastChar=65,
            Widths=Array([1000]),
            Resources=resources,
        )
    )


SHOW = b"q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n"
PAINT = b"q /Pattern cs /P1 scn 0 0 100 100 re f Q\n"
DRAW = b"/R Do\n"


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


add_page(PAINT, Dictionary(Pattern=Dictionary(P1=tiling(DRAW, Dictionary(XObject=Dictionary(R=reference()))))))

add_page(SHOW, Dictionary(Font=Dictionary(T3=type3(DRAW, Dictionary(XObject=Dictionary(R=reference()))))))

glyph3 = type3(DRAW, Dictionary(XObject=Dictionary(R=reference())))
add_page(PAINT, Dictionary(Pattern=Dictionary(P1=tiling(SHOW, Dictionary(Font=Dictionary(T3=glyph3))))))

add_page(b"/R Do /R Do\n", Dictionary(XObject=Dictionary(R=reference())))

appearance = form(DRAW, Dictionary(XObject=Dictionary(R=reference())))
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([0, 0, 20, 20]),
        F=4,
        Contents=String("A square"),
        AP=Dictionary(N=appearance),
    )
)
add_page(b"0 g 0 0 10 10 re f\n", Dictionary(), Array([annot]))

listed = reference()
cell6 = tiling(b"0 g 0 0 10 10 re f\n", Dictionary(XObject=Dictionary(R=listed)))
glyph6 = type3(b"0 0 1000 1000 re f\n", Dictionary(XObject=Dictionary(R=listed)))
add_page(
    PAINT + SHOW,
    Dictionary(XObject=Dictionary(R=listed), Pattern=Dictionary(P1=cell6), Font=Dictionary(T3=glyph6)),
)

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "refreach_ua1.pdf")
