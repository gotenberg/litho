#!/usr/bin/env python3
"""Build sharednested_2b.pdf: nested streams two pages share and bind apart.

ISO 19005-2, 6.2.2 requires every content stream to have an explicitly
associated resource dictionary, and test 2 fails a form XObject or a tiling
pattern cell that resolves a name only through the page's. When two pages draw
the same form, each page may bind that name to a different object, so a repair
that copies one page's binding into the shared form repaints the other page
with it. litho's remediation left every shared form and pattern alone; no corpus
file draws one from two pages, so nothing measured the repair.

| Object | Construct                                                    | Pages bind it |
| ------ | ------------------------------------------------------------ | ------------- |
| X0     | form XObject, `/Resources << >>`, `/GS0 gs`                  | apart         |
| X1     | form XObject, `/Resources << >>`, `/GS1 gs`                  | alike         |
| P0     | tiling pattern, `/Resources << >>`, `/GS0 gs`                | apart         |

Page 1 binds `/GS0` to a graphics state setting a line width of 2, page 2 to
one setting 4; both bind `/GS1` to the same object. The pages share one
indirect `/XObject` dictionary, so a repair that repoints X0 there for one page
repoints it for the other too. The pages define every name, so their own
content fails nothing, and test 2 counts the three nested streams.

A CMYK OutputIntent covers the initial DeviceGray every stream paints in, and
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

thin = pdf.make_indirect(Dictionary(Type=Name.ExtGState, LW=2))
thick = pdf.make_indirect(Dictionary(Type=Name.ExtGState, LW=4))
both = pdf.make_indirect(Dictionary(Type=Name.ExtGState, LC=1))


def form(content):
    """One form XObject with an empty /Resources."""
    xo = Stream(pdf, content)
    xo.Type = Name.XObject
    xo.Subtype = Name.Form
    xo.BBox = Array([0, 0, 50, 50])
    xo.Resources = Dictionary()
    return pdf.make_indirect(xo)


pattern = Stream(pdf, b"/GS0 gs 0 0 5 5 re S\n")
pattern.Type = Name.Pattern
pattern.PatternType = 1
pattern.PaintType = 1
pattern.TilingType = 1
pattern.BBox = Array([0, 0, 10, 10])
pattern.XStep = 10
pattern.YStep = 10
pattern.Resources = Dictionary()
pattern = pdf.make_indirect(pattern)

xobjects = pdf.make_indirect(
    Dictionary(
        X0=form(b"/GS0 gs 0 0 50 50 re S\n"),
        X1=form(b"/GS1 gs 0 0 50 50 re S\n"),
    )
)

content = (
    b"q /Pattern cs /P0 scn 0 700 50 50 re f Q\n"
    b"q 1 0 0 1 0 600 cm /X0 Do Q\n"
    b"q 1 0 0 1 60 600 cm /X1 Do Q\n"
)


def page(gs0):
    """One page drawing every shared stream, binding /GS0 to gs0."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(
                ExtGState=Dictionary(GS0=gs0, GS1=both),
                Pattern=Dictionary(P0=pattern),
                XObject=xobjects,
            ),
            Contents=pdf.make_indirect(Stream(pdf, content)),
        )
    )


pdf.pages.append(pikepdf.Page(page(thin)))
pdf.pages.append(pikepdf.Page(page(thick)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "sharednested_2b.pdf")
