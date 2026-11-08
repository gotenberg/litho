#!/usr/bin/env python3
"""Build realglyphs_ua1.pdf: one Type 3 glyph shown in real content and beside it.

veraPDF 1.30.2 keys a Glyph by the stream it is shown in being real content.
A PDF/UA or WTPDF validation parses a page's content, and the forms it draws,
as semantic content (real), and a tiling cell, a Type 3 glyph procedure, an
annotation appearance and a form one of those draws as plain content (not
real). A glyph with no Unicode value shown in both is two glyphs there, and
one at PDF/A, which parses nothing as semantic. A cell starts in the text
rendering mode the stream selecting its pattern starts in, so a Type 3 font
can show its own glyphs again in a cell one of its procedures selects. No
corpus file the gate validates reaches these shapes, so this fixture plants
one per page, each through Type 3 fonts of its own whose glyph names carry no
Unicode value:

| Page | The page shows the glyph, and so does                                 | ISO 14289-1, 7.21.7 t1 | ISO 19005-2, 6.2.11.7.2 t1 |
| ---- | ---------------------------------------------------------------------- | ---------------------- | -------------------------- |
| 1    | a cell the page selects                                                | 2                      | 1                          |
| 2    | an annotation appearance                                               | 2                      | 1                          |
| 3    | a procedure of a second Type 3 font the page shows                     | 3                      | 2                          |
| 4    | a form a cell draws                                                    | 2                      | 1                          |
| 5    | a form the page draws (control)                                        | 1                      | 1                          |
| 6    | nothing; a cell the glyph's own procedure selects shows the other glyph | 2                      | 2                          |
| 7    | a cell the glyph's own procedure selects, the page showing it in mode 2 | 2                      | 1                          |

Page 3 counts the second font's glyph too. Page 5 is the control: a form the
page draws is real content, so its glyph is the page's. Pages 6 and 7 hold the
same cycle, a procedure selecting a cell that shows its own font: the cell's
other glyph is judged as any other (page 6), and the cell starting in the
procedure's rendering mode 2 makes the page's glyph the cell's (page 7).

A CMYK OutputIntent covers the DeviceGray every glyph paints in.

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

PLAIN = b"0 0 1000 1000 re f\n"


def type3(glyphs):
    """A Type 3 font over codes A, B, ... whose glyphs (name, procedure body)
    set their colour (d0). The font's /Resources start empty."""
    procs = Dictionary()
    names = []
    for name, body in glyphs:
        procs[Name("/" + name)] = pdf.make_indirect(Stream(pdf, b"1000 0 d0\n" + body))
        names.append(Name("/" + name))
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=procs,
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65] + names)),
            FirstChar=65,
            LastChar=64 + len(names),
            Widths=Array([1000] * len(names)),
            Resources=Dictionary(),
        )
    )


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


def form(content, resources):
    """A form XObject carrying its own /Resources."""
    fm = Stream(pdf, content)
    fm.Type = Name.XObject
    fm.Subtype = Name.Form
    fm.BBox = Array([0, 0, 20, 20])
    fm.Resources = resources
    return pdf.make_indirect(fm)


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


SHOW = b"BT /T 10 Tf 10 10 Td (A) Tj ET\n"
FILL = b"q /Pattern cs /P scn 0 0 100 100 re f Q\n"
CELL = b"BT /T 4 Tf (A) Tj ET\n"

# Page 1: the page and a cell it selects.
t1 = type3([("q1", PLAIN)])
add_page(SHOW + FILL, Dictionary(Font=Dictionary(T=t1), Pattern=Dictionary(P=tiling(CELL, Dictionary(Font=Dictionary(T=t1))))))

# Page 2: the page and an annotation appearance.
t2 = type3([("q2", PLAIN)])
appearance = form(CELL, Dictionary(Font=Dictionary(T=t2)))
annot = pdf.make_indirect(
    Dictionary(Type=Name.Annot, Subtype=Name.Square, Rect=Array([10, 10, 30, 30]), F=4, AP=Dictionary(N=appearance))
)
add_page(SHOW, Dictionary(Font=Dictionary(T=t2)), annots=Array([annot]))

# Page 3: the page and a procedure of a second Type 3 font the page shows.
t3 = type3([("q3", PLAIN)])
t3b = type3([("h3", CELL)])
t3b.Resources = Dictionary(Font=Dictionary(T=t3))
add_page(SHOW + b"BT /U 10 Tf 50 50 Td (A) Tj ET\n", Dictionary(Font=Dictionary(T=t3, U=t3b)))

# Page 4: the page and a form a cell draws.
t4 = type3([("q4", PLAIN)])
cell4 = tiling(b"/Fm Do\n", Dictionary(XObject=Dictionary(Fm=form(CELL, Dictionary(Font=Dictionary(T=t4))))))
add_page(SHOW + FILL, Dictionary(Font=Dictionary(T=t4), Pattern=Dictionary(P=cell4)))

# Page 5 (control): the page and a form the page draws.
t5 = type3([("q5", PLAIN)])
add_page(SHOW + b"/Fm Do\n", Dictionary(Font=Dictionary(T=t5), XObject=Dictionary(Fm=form(CELL, Dictionary(Font=Dictionary(T=t5))))))

# Page 6: the glyph's own procedure selects a cell showing the font's other glyph.
t6 = type3([("q6", b"/Pattern cs /P scn " + PLAIN), ("r6", PLAIN)])
t6.Resources = Dictionary(Pattern=Dictionary(P=tiling(b"BT /T 4 Tf (B) Tj ET\n", Dictionary(Font=Dictionary(T=t6)))))
add_page(SHOW, Dictionary(Font=Dictionary(T=t6)))

# Page 7: the same cycle, the page showing the glyph in rendering mode 2.
t7 = type3([("q7", b"/Pattern cs /P scn " + PLAIN)])
t7.Resources = Dictionary(Pattern=Dictionary(P=tiling(CELL, Dictionary(Font=Dictionary(T=t7)))))
add_page(b"BT 2 Tr /T 10 Tf 10 10 Td (A) Tj ET\n", Dictionary(Font=Dictionary(T=t7)))

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "realglyphs_ua1.pdf")
