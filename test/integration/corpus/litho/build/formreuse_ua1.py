#!/usr/bin/env python3
"""Build formreuse_ua1.pdf: marked forms a cell, a glyph, a form or an appearance references again.

ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
a form's /StructParents names the one parent tree entry its marked-content
sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4), which a second
reference paints again under the same parents. veraPDF 1.30.2 states test 2
on the form object a Do or an appearance entry builds: a form carrying
/StructParents passes at its first reference and fails at every later one. A content stream
is parsed once for the document, so a Do counts once however many pages or
streams draw the stream holding it, and an appearance entry counts once per
annotation. litho counted the Do operators of the pages' own content alone. No
corpus file references a marked form twice at PDF/UA-1, so this fixture plants
one path per page, each with a marked form of its own, and every path but the
page's own content:

| Page | Path                                                 | Answers at PDF/UA-1 |
| ---- | ---------------------------------------------------- | ------------------- |
| 1    | the page and a tiling cell it selects, `/F Do` each  | `7.20/2` once       |
| 2    | the page and a Type 3 glyph it shows, `/F Do` each   | `7.20/2` once       |
| 3    | the page and a form it draws, `/F Do` each           | `7.20/2` once       |
| 4    | the normal appearance of two annotations             | `7.20/2` once       |
| 5    | one annotation's normal and down appearance          | `7.20/2` once       |
| 6    | the page's `/F Do` and an annotation's appearance    | `7.20/2` once       |
| 7    | a form, a cell and a glyph drawn twice, each `/F Do` | nothing             |

Page 7 is the must-not-fire control: it draws twice a form that draws a marked
form once, selects twice a cell that draws another, shows twice a glyph that
draws a third, and draws twice a form without /StructParents and a form
carrying /StructParent.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

See ../README.md for the authoritative expected firing set.
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

MARKED = b"/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"


def form(content, resources=None, key=None):
    """A form XObject; key names the structure entry it carries, if any."""
    xobj = Stream(pdf, content)
    xobj.Type = Name.XObject
    xobj.Subtype = Name.Form
    xobj.BBox = Array([0, 0, 20, 20])
    xobj.Resources = resources if resources is not None else Dictionary()
    if key is not None:
        xobj[Name("/" + key)] = 0
    return pdf.make_indirect(xobj)


def marked():
    """A form whose content is one marked-content sequence with an MCID."""
    return form(MARKED, key="StructParents")


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


def annot(appearances):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Square,
            Rect=Array([0, 0, 20, 20]),
            F=4,
            Contents=String("A square"),
            AP=Dictionary(appearances),
        )
    )


SHOW = b"q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n"
PAINT = b"q /Pattern cs /P1 scn 0 0 100 100 re f Q\n"
DRAW = b"/F Do\n"
PLAIN = b"0 g 0 0 10 10 re f\n"


def add_page(content, resources, annots=None, contents=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=resources,
        Contents=contents if contents is not None else pdf.make_indirect(Stream(pdf, content)),
    )
    if annots is not None:
        page.Annots = Array(annots)
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


def draws(f):
    return Dictionary(XObject=Dictionary(F=f))


f1 = marked()
add_page(DRAW + PAINT, Dictionary(XObject=Dictionary(F=f1), Pattern=Dictionary(P1=tiling(DRAW, draws(f1)))))

f2 = marked()
add_page(SHOW + DRAW, Dictionary(XObject=Dictionary(F=f2), Font=Dictionary(T3=type3(DRAW, draws(f2)))))

f3 = marked()
add_page(b"/G Do\n" + DRAW, Dictionary(XObject=Dictionary(F=f3, G=form(DRAW, draws(f3)))))

f4 = marked()
add_page(PLAIN, Dictionary(), [annot({"/N": f4}), annot({"/N": f4})])

f5 = marked()
add_page(PLAIN, Dictionary(), [annot({"/N": f5, "/D": f5})])

f6 = marked()
add_page(DRAW, draws(f6), [annot({"/N": f6})])

add_page(
    b"/G Do /G Do\n" + PAINT + PAINT + SHOW + SHOW + b"/N Do /N Do /S Do /S Do\n",
    Dictionary(
        XObject=Dictionary(
            G=form(DRAW, draws(marked())),
            N=form(MARKED),
            S=form(MARKED, key="StructParent"),
        ),
        Pattern=Dictionary(P1=tiling(DRAW, draws(marked()))),
        Font=Dictionary(T3=type3(DRAW, draws(marked()))),
    ),
)

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "formreuse_ua1.pdf")
