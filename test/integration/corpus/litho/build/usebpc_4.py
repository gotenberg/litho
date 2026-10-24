#!/usr/bin/env python3
"""Build usebpc_4.pdf: ICC colours under each /UseBlackPtComp value, PDF/A-4.

PDF 2.0 adds the ExtGState entry /UseBlackPtComp (ISO 32000-2, Table 57):
/ON, /OFF, or /Default, its initial value, which leaves black point
compensation to the reader. The two corpus files that carry it never reach an
ICC profile under /OFF (pdf20 `PDF_2.0_image_with_BPC` is CalRGB under /ON,
pdfjs `issue17794` states /Default), so this fixture draws ICC colours whose
profiles have a black lighter than sRGB's under all three values.

Three 100 x 200 pages, /Default (no gs), /ON and /OFF in that order, paint
the same three things, each 80 points wide from x = 10:

| Row       | Draws                                                             |
| --------- | ----------------------------------------------------------------- |
| y 140-190 | `Gy`, an 8 x 8 grey ramp, samples 0 to 224, under profile `grey`  |
| y 80-130  | `Rg`, an 8 x 8 RGB ramp from black, under profile `rgb`           |
| y 20-70   | a fill of `0 sc` in `[/ICCBased grey]`                            |

Both profiles are version 2.1 display profiles whose tone curves start at a
luminance of 1/8 (a two-entry sampled curve from 0x2000), so their black reads
L* 42. Black point compensation (ISO 18619) maps it onto sRGB's black;
without it the black draws as a mid grey. One state per page keeps the pages
that compensate apart from the one that does not, so each is judged alone.
/OFF comes last, on page 3: the corpus gate's redact-render scenarios redact
pages 1 and 2 and judge a blanked image by its darkness over black, which a
blank under /OFF, drawn L* 42, cannot reach (the library's pixel check reads
it compensated).

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, PRIMARIES, icc_profile, text_description, xyz

pdf = pikepdf.new()


def lifted_curve():
    """A curveType (ICC.1:2001-04, 6.5.3) of two entries: linear from 1/8."""
    return b"curv" + bytes(4) + struct.pack(">IHH", 2, 0x2000, 0xFFFF)


def grey_profile():
    return icc_profile(
        b"mntr",
        b"GRAY",
        b"XYZ ",
        [(b"desc", text_description("litho lifted grey"))] + COMMON_TAGS + [(b"kTRC", lifted_curve())],
    )


def rgb_profile():
    return icc_profile(
        b"mntr",
        b"RGB ",
        b"XYZ ",
        [(b"desc", text_description("litho lifted RGB"))]
        + COMMON_TAGS
        + [(sig, xyz(v)) for sig, v in PRIMARIES.items()]
        + [(sig, lifted_curve()) for sig in (b"rTRC", b"gTRC", b"bTRC")],
    )


def profile_stream(data, components):
    stream = Stream(pdf, data)
    stream.N = components
    return pdf.make_indirect(stream)


grey = pdf.make_indirect(Array([Name.ICCBased, profile_stream(grey_profile(), 1)]))
rgb = pdf.make_indirect(Array([Name.ICCBased, profile_stream(rgb_profile(), 3)]))


def image(space, row, components):
    """An image of ROWS identical rows of samples row."""
    stream = Stream(pdf, bytes(row) * ROWS)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = len(row) // components
    stream.Height = ROWS
    stream.BitsPerComponent = 8
    stream.ColorSpace = space
    return pdf.make_indirect(stream)


# Eight rows, so a region over part of an image covers whole samples.
ROWS = 8
ramp = [32 * i for i in range(8)]
grey_image = image(grey, ramp, 1)
# A ramp on red alone, from the neutral black of its first sample.
rgb_image = image(rgb, [v for i in ramp for v in (i, 0, 0)], 3)

states = Dictionary(
    Off=Dictionary(Type=Name.ExtGState, UseBlackPtComp=Name.OFF),
    On=Dictionary(Type=Name.ExtGState, UseBlackPtComp=Name.ON),
)
for gs in [b"", b"/On gs ", b"/Off gs "]:
    content = (
        b"q %s"
        b"q 80 0 0 50 10 140 cm /Gy Do Q "
        b"q 80 0 0 50 10 80 cm /Rg Do Q "
        b"/G cs 0 sc 10 20 80 50 re f Q\n" % gs
    )
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 100, 200]),
            Resources=Dictionary(
                ColorSpace=Dictionary(G=grey),
                XObject=Dictionary(Gy=grey_image, Rg=rgb_image),
                ExtGState=states,
            ),
            Contents=Stream(pdf, content),
        )
    )
    pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=4, rev=2020))
save(pdf, "usebpc_4.pdf", min_version="2.0")
