#!/usr/bin/env python3
"""Build greyintent_2b.pdf: transparency on pages whose group names no
blending colour space, under a grey OutputIntent, at PDF/A-2b.

ISO 32000-1, 11.4.7 and Table 147 leave the blending colour space of a page
group with no /CS to the reader, and 14.11.5 has the output intent describe
the device the document is prepared for, so `Writer.FlattenTransparency`
composites such a page in the intent's device space: DeviceGray here. The
corpora's 30 files with a grey intent flatten nothing. Every colour is
DeviceGray, which the intent characterises:

| Page | Draws                                                                   |
| ---- | ----------------------------------------------------------------------- |
| 1    | no `/Group`: a light square at ca 0.5 over a dark backdrop              |
| 2    | a `/Group` with no `/CS`: a light square Multiply over a mid backdrop   |

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
from _icc import COMMON_TAGS, gamma_curve, icc_profile, text_description

pdf = pikepdf.new()


def grey_display_profile():
    """A version 2.1 grey display profile: one tone curve to the PCS."""
    return icc_profile(
        b"mntr",
        b"GRAY",
        b"XYZ ",
        [(b"desc", text_description("litho grey display"))] + COMMON_TAGS + [(b"kTRC", gamma_curve(2.2))],
    )


destination = Stream(pdf, grey_display_profile())
destination.N = 1
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho grey"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)


def add_page(content, states, group=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 100]),
        Resources=Dictionary(ExtGState=states),
        Contents=Stream(pdf, content),
    )
    if group is not None:
        page.Group = group
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


add_page(
    b"0.2 g 0 0 100 100 re f q /A gs 0.8 g 20 20 50 50 re f Q\n",
    Dictionary(A=Dictionary(Type=Name.ExtGState, ca=0.5)),
)
add_page(
    b"0.5 g 0 0 100 100 re f q /M gs 0.6 g 20 20 50 50 re f Q\n",
    Dictionary(M=Dictionary(Type=Name.ExtGState, BM=Name.Multiply)),
    Dictionary(Type=Name.Group, S=Name.Transparency),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "greyintent_2b.pdf", min_version="1.7")
