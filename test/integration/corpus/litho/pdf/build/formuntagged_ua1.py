#!/usr/bin/env python3
"""Build formuntagged_ua1.pdf: an untagged document whose widgets paint, at
PDF/UA-1.

ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
of a document claiming conformance to be tagged as real content or marked as
an artifact. A widget is not page content, so a document with no structure
tree passes those tests while its page draws nothing. Flattening the form
draws each appearance into the page, where there is no structure element to
tag it under, so in a document claiming PDF/UA, as this one does, litho marks
it as an artifact (ISO 32000-1, 14.8.2.2). The other litho fixtures with
painting widgets are tagged, and the untagged ones paint nothing or claim no
PDF/UA, so this fixture gives the gate the shape:

| Field   | Kind       | /Rect             | Appearance                              |
| ------- | ---------- | ----------------- | --------------------------------------- |
| name    | text       | [40 220 240 250]  | a light box, a dark corner and a border |
| agree   | check box  | [40 170 60 190]   | /On: a filled square; /Off: a border    |
| comment | text       | [40 60 360 140]   | the box, its /BBox offset from 0,0      |

The page's own content draws nothing. The file has no /StructTreeRoot and no
/MarkInfo; the XMP packet claims PDF/UA-1, and an RGB OutputIntent covers the
DeviceRGB the appearances paint.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
from _icc import rgb_display_profile

PRINT = 4

pdf = pikepdf.new()

destination = Stream(pdf, rgb_display_profile())
destination.N = 3
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho RGB display"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)


def form(content, bbox):
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array(bbox)
    s.Resources = Dictionary()
    return pdf.make_indirect(s)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        MediaBox=Array([0, 0, 400, 300]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"")),
    )
)
pdf.Root.Pages.Kids = Array([page])
pdf.Root.Pages.Count = 1

name = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        FT=Name.Tx,
        T=String("name"),
        TU=String("Name"),
        Rect=Array([40, 220, 240, 250]),
        P=page,
        F=PRINT,
        AP=Dictionary(
            N=form(
                b"0.9 0.93 1 rg 0 0 200 30 re f 0 0 0.5 rg 0 0 30 10 re f 0.7 0 0 RG 2 w 1 1 198 28 re S\n",
                [0, 0, 200, 30],
            )
        ),
    )
)
agree = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        FT=Name.Btn,
        T=String("agree"),
        TU=String("I agree"),
        V=Name.On,
        AS=Name.On,
        Rect=Array([40, 170, 60, 190]),
        P=page,
        F=PRINT,
        AP=Dictionary(
            N=Dictionary(
                On=form(b"0 0 0 RG 1 w 0.5 0.5 19 19 re S 0 0.4 0 rg 4 4 12 12 re f\n", [0, 0, 20, 20]),
                Off=form(b"0 0 0 RG 1 w 0.5 0.5 19 19 re S\n", [0, 0, 20, 20]),
            )
        ),
    )
)
comment = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        FT=Name.Tx,
        T=String("comment"),
        TU=String("Comment"),
        Rect=Array([40, 60, 360, 140]),
        P=page,
        F=PRINT,
        AP=Dictionary(
            N=form(
                b"1 0.95 0.85 rg 100 100 320 80 re f 0 0 0.5 rg 100 100 40 20 re f 0.7 0 0 RG 2 w 101 101 318 78 re S\n",
                [100, 100, 420, 180],
            )
        ),
    )
)

page.Annots = Array([name, agree, comment])
pdf.Root.AcroForm = Dictionary(Fields=Array([name, agree, comment]))
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "formuntagged_ua1.pdf")
