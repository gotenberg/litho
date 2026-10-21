#!/usr/bin/env python3
"""Build flattenrefuse_1b.pdf: transparency on two pages, one of which
transparency flattening refuses, at PDF/A-1b.

A PDF/A-1 remediation flattens the transparency ISO 19005-1, 6.4 forbids, page
by page when a page refuses, and sets opaque what it leaves. The corpora hold
no page flattening refuses: every page they flatten fits the render pixel
budget at some resolution. This file holds one beside one that flattens:

| Page | Size (pt)   | Draws                                                      |
| ---- | ----------- | ---------------------------------------------------------- |
| 1    | 612 x 792   | a red square at ca 0.5 over a blue band                    |
| 2    | 2000 x 2000 | the same, the square inside eight nested isolated knockout |
|      |             | groups                                                     |

Flattening composites page 2 with two page-sized planes for each open
isolated knockout group, which no renderer can elide. Eight of them pass the
50 million pixel render budget at every resolution flattening tries, down to
72 dpi, so it refuses the page (`flatten.ErrTooComplex`), and the remediation
flattens page 1 alone and sets page 2's transparency opaque. A renderer
drawing page 2 without the groups draws the same page: each group holds one
element, composited over the group's transparent initial backdrop and the
result over the page beneath, as painting it directly does (ISO 32000-1,
11.4.6 and 11.4.7).

The file declares no OutputIntent, so the DeviceRGB paint fails 6.2.3.3 t1 at
PDF/A-1b; the groups fail 6.4 t3 and the constant alpha 6.4 t6.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

NESTED_GROUPS = 8
BIG = 2000

half = pdf.make_indirect(Dictionary(Type=Name.ExtGState, ca=0.5))
BAND = b"0 0 1 rg 50 300 400 100 re f\n"
SQUARE = b"q /Half gs 1 0 0 rg 150 250 200 200 re f Q\n"


def group_form(content, resources, size):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, size, size])
    stream.Group = Dictionary(Type=Name.Group, S=Name.Transparency, I=True, K=True)
    stream.Resources = resources
    return pdf.make_indirect(stream)


def page(size, content, resources):
    pdf.pages.append(
        pikepdf.Page(
            pdf.make_indirect(
                Dictionary(
                    Type=Name.Page,
                    MediaBox=Array([0, 0, size[0], size[1]]),
                    Resources=resources,
                    Contents=Stream(pdf, content),
                )
            )
        )
    )


page((612, 792), BAND + SQUARE, Dictionary(ExtGState=Dictionary(Half=half)))

inner = group_form(SQUARE, Dictionary(ExtGState=Dictionary(Half=half)), BIG)
for _ in range(NESTED_GROUPS - 1):
    inner = group_form(b"/G Do\n", Dictionary(XObject=Dictionary(G=inner)), BIG)
page((BIG, BIG), BAND + b"/G Do\n", Dictionary(XObject=Dictionary(G=inner)))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))

save(pdf, "flattenrefuse_1b.pdf", min_version="1.4")
