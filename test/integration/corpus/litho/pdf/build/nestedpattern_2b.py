#!/usr/bin/env python3
"""Build nestedpattern_2b.pdf: tiling pattern names only the page defines.

ISO 19005-2, 6.2.2 requires every content stream to have an explicitly
associated resource dictionary, and test 2 fails a stream that resolves a name
only through the page's. A tiling pattern's cell is a content stream with a
/Resources entry of its own (ISO 32000-1, 8.7.3.1, Table 75), and so is a form
XObject. veraPDF 1.30.2 fails test 2 on a name a cell finds only on the page,
with the pattern's /Resources empty or absent alike, and on a form XObject whose
`scn` selects a pattern only the page defines. No corpus file reaches either,
and litho's remediation gave form XObjects and Type 3 fonts the names they
borrow, never a pattern's cell nor a pattern a form paints.

| Object | Construct                                            | Answers   |
| ------ | ---------------------------------------------------- | --------- |
| P0     | tiling pattern, `/Resources << >>`, `/GS0 gs`         | `6.2.2/2` |
| P1     | tiling pattern, no `/Resources`, `/GS1 gs`            | `6.2.2/2` |
| P2     | tiling pattern holding its own `/GS0`                | silent    |
| X0     | form XObject, `/Resources << >>`, `/P3 scn`           | `6.2.2/2` |
| X1     | form XObject holding its own `/P3`                   | silent    |
| P3     | tiling pattern the two forms paint, naming nothing   | silent    |

P2 and X1 are the over-repair controls: each already holds the name it uses,
so a repair that rewrites either rewrites a conforming object. The page itself
defines every name, so its own content fails nothing, and test 2 counts one
failure for each of P0, P1 and X0.

A CMYK OutputIntent covers the initial DeviceGray every cell paints in, and
the file draws no text, so no colour or font clause fires beside test 2.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

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

gs0 = pdf.make_indirect(Dictionary(Type=Name.ExtGState, CA=1.0))
gs1 = pdf.make_indirect(Dictionary(Type=Name.ExtGState, ca=1.0))


def tiling_pattern(content, resources):
    """One coloured tiling pattern; resources None states no /Resources."""
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = 1
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 10, 10])
    pat.XStep = 10
    pat.YStep = 10
    if resources is not None:
        pat.Resources = resources
    return pdf.make_indirect(pat)


def form(content, resources):
    """One form XObject carrying the resources given."""
    xo = Stream(pdf, content)
    xo.Type = Name.XObject
    xo.Subtype = Name.Form
    xo.BBox = Array([0, 0, 50, 50])
    xo.Resources = resources
    return pdf.make_indirect(xo)


p3 = tiling_pattern(b"0 0 5 5 re f\n", Dictionary())
patterns = Dictionary(
    P0=tiling_pattern(b"/GS0 gs 0 0 5 5 re f\n", Dictionary()),
    P1=tiling_pattern(b"/GS1 gs 0 0 5 5 re f\n", None),
    P2=tiling_pattern(b"/GS0 gs 0 0 5 5 re f\n", Dictionary(ExtGState=Dictionary(GS0=gs0))),
    P3=p3,
)
xobjects = Dictionary(
    X0=form(b"/Pattern cs /P3 scn 0 0 50 50 re f\n", Dictionary()),
    X1=form(b"/Pattern cs /P3 scn 0 0 50 50 re f\n", Dictionary(Pattern=Dictionary(P3=p3))),
)

content = Stream(
    pdf,
    b"q /Pattern cs /P0 scn 0 700 50 50 re f\n"
    b"/P1 scn 60 700 50 50 re f\n"
    b"/P2 scn 120 700 50 50 re f Q\n"
    b"q 1 0 0 1 0 600 cm /X0 Do Q\n"
    b"q 1 0 0 1 60 600 cm /X1 Do Q\n",
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            ExtGState=Dictionary(GS0=gs0, GS1=gs1),
            Pattern=patterns,
            XObject=xobjects,
        ),
        Contents=pdf.make_indirect(content),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "nestedpattern_2b.pdf")
