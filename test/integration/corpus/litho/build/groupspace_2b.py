#!/usr/bin/env python3
"""Build groupspace_2b.pdf: pages whose transparency group blends in a
CMYK or grey space, painting RGB colour, at PDF/A-2b.

ISO 32000-1, 11.4.7 and Table 147 convert every colour painted into a page
group into the group's /CS, so a page whose content paints with transparency
reads in that space: an RGB colour outside the CMYK gamut reads as the CMYK
colour nearest it, and every colour reads grey on a grey page. Flattening
removes the group with the transparency, so what the composites leave of the
page read in sRGB unless converted into the group's space. The corpora held
no page whose group blends in CMYK or grey while painting colour of another
space outside its transparent regions, so the flatten cell compared none.

Each 100 x 200 page draws rows of eight 10 x 50 fills, and a 10 x 5 square at
ca 0.5 in its top right corner, the transparency flattening composites:

| Page | Group /CS                                  | Draws                       |
| ---- | ------------------------------------------ | --------------------------- |
| 1    | DeviceCMYK                                 | the rows below              |
| 2    | ICCBased, version 4 CMYK (v4-cmyk-lut.icc) | the rows below              |
| 3    | DeviceGray                                 | the rows below              |
| 4    | DeviceCMYK                                 | form `F`, the rows below    |
| 5    | none                                       | form `F` again, opaque      |

| Row       | Draws                                                          |
| --------- | -------------------------------------------------------------- |
| y 140-190 | DeviceRGB fills from green to red, outside the CMYK gamut       |
| y 80-130  | dark DeviceRGB greys, v = 0, 0.04 ... 0.28                      |
| y 20-70   | `Im`, an 8 x 1 DeviceRGB image of the first row's colours       |

Page 4 paints its rows through form `F`, which page 5 paints too: converting
the form for page 4 would change page 5, which has no group, so page 4 keeps
its colours (pdf.FlattenedPage.GroupSpaceUnconverted).

See ../README.md for the authoritative expected firing set.
"""

import os
import zlib

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

CMYK_PROFILE = os.path.join(
    os.path.dirname(os.path.abspath(__file__)),
    *[".."] * 5,
    "pkg",
    "internal",
    "graphics",
    "icc",
    "testdata",
    "v4-cmyk-lut.icc",
)

pdf = pikepdf.new()

with open(CMYK_PROFILE, "rb") as f:
    cmyk_stream = Stream(pdf, zlib.compress(f.read(), 9))
cmyk_stream.N = 4
cmyk_stream.Filter = Name.FlateDecode
icc_cmyk = pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(cmyk_stream)]))

colours = [(i / 7, 1 - i / 7, 0.3 * (i % 2)) for i in range(8)]
greys = [0.04 * i for i in range(8)]



def rgb_image():
    """Return a new 8 x 1 DeviceRGB image of the colours: each page has its
    own, which no page converted to another space shares."""
    image = Stream(pdf, bytes(round(255 * c) for rgb in colours for c in rgb))
    image.Type = Name.XObject
    image.Subtype = Name.Image
    image.Width = len(colours)
    image.Height = 1
    image.BitsPerComponent = 8
    image.ColorSpace = Name.DeviceRGB
    return pdf.make_indirect(image)


rows = (
    b"".join(b"%.4f %.4f %.4f rg %d 140 10 50 re f " % (r, g, b, 10 + 10 * i) for i, (r, g, b) in enumerate(colours))
    + b"q "
    + b"".join(b"%.2f %.2f %.2f rg %d 80 10 50 re f " % (v, v, v, 10 + 10 * i) for i, v in enumerate(greys))
    + b"Q q 80 0 0 50 10 20 cm /Im Do Q\n"
)
square = b"q /H gs 0.2 0.4 0.9 rg 90 195 10 5 re f Q\n"

states = Dictionary(H=Dictionary(Type=Name.ExtGState, ca=0.5))


def add_page(content, resources, cs=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 200]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    if cs is not None:
        page.Group = Dictionary(Type=Name.Group, S=Name.Transparency, CS=cs)
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


for cs in (Name.DeviceCMYK, icc_cmyk, Name.DeviceGray):
    add_page(rows + square, Dictionary(ExtGState=states, XObject=Dictionary(Im=rgb_image())), cs)

form = Stream(pdf, rows)
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 100, 200])
form.Resources = Dictionary(XObject=Dictionary(Im=rgb_image()))
form = pdf.make_indirect(form)

add_page(b"/F Do " + square, Dictionary(ExtGState=Dictionary(H=states.H), XObject=Dictionary(F=form)), Name.DeviceCMYK)
add_page(b"/F Do\n", Dictionary(XObject=Dictionary(F=form)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "groupspace_2b.pdf", min_version="1.7")
