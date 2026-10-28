#!/usr/bin/env python3
"""Build flatuntagged_ua1.pdf: an untagged document whose annotations paint
with transparency, at PDF/UA-1.

ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
of a document claiming conformance to be tagged as real content or marked as
an artifact. An annotation is not page content, so a document with no
structure tree passes those tests while its page draws nothing of its own.
Writer.FlattenTransparency draws each annotation that paints with
transparency into the page, where no structure element can take it, so in a
document claiming PDF/UA, as this one does, litho marks the drawing as an
artifact (ISO 32000-1, 14.8.2.2), as FlattenForm marks a baked widget. The
corpora's untagged files that claim PDF/UA hold no annotation painting with
transparency, so this fixture gives the flatten cell the shape:

| Annotation | /Rect             | Appearance                                          |
| ---------- | ----------------- | --------------------------------------------------- |
| Square     | [10 10 60 60]     | a blue square, /CA 0.5 and an ExtGState at ca 0.5   |
| FreeText   | [80 20 190 80]    | a background at ca 0.6 under opaque text "DEA"      |

The page's own content draws nothing. Every glyph is a Type 3 glyph with a
/ToUnicode map. The file has no /StructTreeRoot and no /MarkInfo; the XMP
packet claims PDF/UA-1, and an RGB OutputIntent covers the DeviceRGB the
appearances paint.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, tounicode_cmap, type3_font, xmp_packet
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

GLYPHS = [
    ("A", 600, "0 0 500 700 re f"),
    ("B", 600, "0 0 500 700 re 100 100 300 500 re f*"),
    ("C", 600, "0 0 m 500 0 l 250 700 l f"),
    ("D", 600, "0 300 500 150 re f 175 0 150 700 re f"),
    ("E", 600, "200 0 120 700 re f 0 580 500 120 re f"),
]
font = type3_font(pdf, GLYPHS, [600] * len(GLYPHS))
font.ToUnicode = tounicode_cmap(pdf, {65 + i: ord(name) for i, (name, _, _) in enumerate(GLYPHS)})
font = pdf.make_indirect(font)


def form(content, bbox, resources):
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array(bbox)
    s.Resources = resources
    return pdf.make_indirect(s)


def state(**entries):
    return pdf.make_indirect(Dictionary(Type=Name.ExtGState, **entries))


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        MediaBox=Array([0, 0, 200, 100]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"")),
    )
)
pdf.Root.Pages.Kids = Array([page])
pdf.Root.Pages.Count = 1

square = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([10, 10, 60, 60]),
        P=page,
        F=PRINT,
        CA=0.5,
        C=Array([0, 0, 1]),
        Contents=String("A faded square"),
        AP=Dictionary(N=form(b"/H gs 0 0 1 rg 0 0 50 50 re f\n", [0, 0, 50, 50], Dictionary(ExtGState=Dictionary(H=state(ca=0.5))))),
    )
)
free_text = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.FreeText,
        Rect=Array([80, 20, 190, 80]),
        P=page,
        F=PRINT,
        Contents=String("DEA"),
        DA=String("0 0 1 rg /F 16 Tf"),
        AP=Dictionary(
            N=form(
                b"q /T gs 0.8 0.9 1 rg 0 0 110 60 re f Q BT /F 16 Tf 0 0 1 rg 10 22 Td (DEA) Tj ET\n",
                [0, 0, 110, 60],
                Dictionary(Font=Dictionary(F=font), ExtGState=Dictionary(T=state(ca=0.6))),
            )
        ),
    )
)

page.Annots = Array([square, free_text])
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "flatuntagged_ua1.pdf")
