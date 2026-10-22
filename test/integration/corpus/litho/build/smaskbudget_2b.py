#!/usr/bin/env python3
"""Build smaskbudget_2b.pdf: a graphics-state soft mask and a transparency
group whose rasters pass the render pixel budget, at PDF/A-2b.

A renderer that cannot afford a soft mask's group (ISO 32000-1, 11.6.5.2) or
a transparency group (11.4) must not paint what they hide. No corpus file the
render goldens cover reached the budget: pdfjs issue14982.pdf does, at 800 x
5424 pt, and has no render golden. Each page is 4000 x 4000 pt, 16 million
pixels at 72 dpi, under litho's default budget of 50 million; a non-isolated
group over it holds four planes and a layer, 80 million:

| Page | Draws                                                                    |
| ---- | ------------------------------------------------------------------------ |
| 1    | a blue page, then red over it through a luminosity soft mask whose      |
|      | group, over the whole page, is white in a central square and grey 0.5   |
|      | in a band below it: red in the square, purple in the band, blue around  |
| 2    | a blue page, then at ca 0.5 a non-isolated group over the whole page    |
|      | holding a red square: purple in the square                              |

litho renders page 1's mask group at half the resolution, and paints page 2's
group content into the page at the group's alpha. Before, both painted the
red opaque and unmasked.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

SIZE = 4000
BLUE_PAGE = b"0 0 1 rg 0 0 4000 4000 re f\n"


def form(content, group, resources=None):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, SIZE, SIZE])
    stream.Group = group
    if resources is not None:
        stream.Resources = resources
    return pdf.make_indirect(stream)


def add_page(content, resources):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, SIZE, SIZE]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


def state(**entries):
    return Dictionary(Type=Name.ExtGState, **entries)


mask_group = form(
    b"1 g 1000 1500 2000 2000 re f 0.5 g 1000 500 2000 800 re f\n",
    Dictionary(Type=Name.Group, S=Name.Transparency, CS=Name.DeviceGray),
)
add_page(
    BLUE_PAGE + b"/M gs 1 0 0 rg 0 0 4000 4000 re f\n",
    Dictionary(ExtGState=Dictionary(M=state(SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=mask_group)))),
)

group = form(b"1 0 0 rg 1000 1000 2000 2000 re f\n", Dictionary(Type=Name.Group, S=Name.Transparency))
add_page(
    BLUE_PAGE + b"/A gs /G Do\n",
    Dictionary(ExtGState=Dictionary(A=state(ca=0.5)), XObject=Dictionary(G=group)),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "smaskbudget_2b.pdf", min_version="1.7")
