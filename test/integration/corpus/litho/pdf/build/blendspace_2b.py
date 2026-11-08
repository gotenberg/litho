#!/usr/bin/env python3
"""Build blendspace_2b.pdf: transparency composited in a CMYK and a grey
blending colour space, at PDF/A-2b.

ISO 32000-1, 11.3.4 composites a transparency group's elements in the group's
blending colour space, the space its /CS names (Table 147): a blend mode in a
CMYK group applies to the complements of the inks (11.3.5), and its
non-separable modes take K from one side. The corpora's render goldens held no
CMYK group whose content blends, so the gate compared none. Each page draws
in DeviceCMYK, read through litho's built-in CMYK profile as poppler reads it:

| Page | Draws                                                                    |
| ---- | ------------------------------------------------------------------------ |
| 1    | in a page group over DeviceCMYK: an isolated CMYK group, a cyan square   |
|      | then a magenta one Multiply over its right half, and an isolated         |
|      | knockout CMYK group of the same squares Multiply at ca 0.7 over yellow   |
| 2    | a luminosity mask whose group blends in DeviceGray, cyan then magenta    |
|      | Multiply, over a black fill                                              |
| 3    | in a page group over DeviceCMYK: black ink and cyan ink at ca 0.5, each  |
|      | over the other, which flattening keeps as inks                           |

poppler 26.09.0 and PDFium blend every group in RGB, so they draw the overlap
of page 1 as the RGB product of cyan and magenta where ISO 32000-1 makes it
the colour of both inks.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def form(content, bbox, group, resources):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array(bbox)
    stream.Group = group
    stream.Resources = resources
    return pdf.make_indirect(stream)


def transparency(**attrs):
    return Dictionary(Type=Name.Group, S=Name.Transparency, **attrs)


def add_page(content, resources, group=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 100]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    if group is not None:
        page.Group = group
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


def state(**entries):
    return Dictionary(Type=Name.ExtGState, **entries)


MULTIPLY = Dictionary(ExtGState=Dictionary(M=state(BM=Name.Multiply)))
multiply = form(
    b"1 0 0 0 k 0 0 60 50 re f /M gs 0 1 0 0 k 40 0 60 50 re f\n",
    [0, 0, 100, 50],
    transparency(I=True, CS=Name.DeviceCMYK),
    MULTIPLY,
)
knockout = form(
    b"/K gs 1 0 0 0 k 5 5 55 40 re f 0 1 0 0 k 40 5 55 40 re f\n",
    [0, 0, 100, 50],
    transparency(I=True, K=True, CS=Name.DeviceCMYK),
    Dictionary(ExtGState=Dictionary(K=state(BM=Name.Multiply, ca=0.7))),
)
add_page(
    b"q 1 0 0 1 0 50 cm /C Do Q 0 0 1 0 k 0 0 100 50 re f /K Do\n",
    Dictionary(XObject=Dictionary(C=multiply, K=knockout)),
    transparency(CS=Name.DeviceCMYK),
)

mask_group = form(
    b"1 0 0 0 k 0 0 100 100 re f /M gs 0 1 0 0 k 0 0 100 100 re f\n",
    [0, 0, 100, 100],
    transparency(CS=Name.DeviceGray),
    MULTIPLY,
)
add_page(
    b"/L gs 0 0 0 1 k 0 0 100 100 re f\n",
    Dictionary(ExtGState=Dictionary(L=state(SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=mask_group)))),
    transparency(CS=Name.DeviceCMYK),
)

add_page(
    b"/A gs 0 0 0 1 k 10 10 50 80 re f 1 0 0 0 k 40 10 50 80 re f\n",
    Dictionary(ExtGState=Dictionary(A=state(ca=0.5))),
    transparency(CS=Name.DeviceCMYK),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "blendspace_2b.pdf", min_version="1.7")
