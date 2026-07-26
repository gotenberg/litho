#!/usr/bin/env python3
"""Build multipage_2b.pdf: a PDF/A-2b fixture spreading violations across pages.

Every other fixture in this corpus is one page, so none of them can catch a
page-scoped check that stops after the first page, or one that reports a
document-level verdict from page one and never revisits. This fixture isolates
that axis: each page carries a violation class that appears on no other page,
so the reported rule set is only complete if every page was reached.

Page 1 also carries a conforming counterpart of the constructs that fail on
pages 2 and 3, so a check that scans only page 1 stays silent rather than
firing, which makes the failure mode a missing rule rather than a wrong one.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    pikepdf,
    rgb_image,
    save,
    unembedded_truetype,
)

pdf = pikepdf.new()

font = pdf.make_indirect(unembedded_truetype())

# Page 1 paints DeviceCMYK and nothing else. ISO 19005-2, 6.2.4.3 t3 needs a
# CMYK output intent, and this file declares none.
page1_content = Stream(pdf, b"0 0 0 1 k\n50 700 100 40 re f\n")

# Page 2 uses an invalid rendering intent (6.2.6 t1) and an image carrying
# /Interpolate true (6.2.8 t3). Neither construct appears on page 1 or 3.
img_interp = rgb_image(pdf, interpolate=True)
page2_content = Stream(
    pdf,
    b"q\n/BadIntent ri\n"
    b"q 200 0 0 100 50 400 cm /Im1 Do Q\n"
    b"Q\n",
)

# Page 3 draws a PostScript XObject (6.2.9 t3) and a transparency group with
# no blending colour space (6.2.10 t2). Again unique to this page.
psx = Stream(pdf, b"%!PS-Adobe-3.0\n0 0 moveto 10 10 lineto stroke\n")
psx.Type = Name.XObject
psx.Subtype = Name.PS

group_form = Stream(pdf, b"0 0 1 rg\n0 0 50 50 re f\n")
group_form.Type = Name.XObject
group_form.Subtype = Name.Form
group_form.BBox = Array([0, 0, 50, 50])
group_form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
group_form.Resources = Dictionary()

page3_content = Stream(
    pdf,
    b"q 1 0 0 1 50 420 cm /PSX Do Q\nq 1 0 0 1 50 300 cm /FmGrp Do Q\n",
)

# The font is referenced from page 3 only. ISO 19005-2, 6.2.11.4.1 t1 is
# reported per rendered font, so a walk that stops early never sees it.
page3_content = Stream(
    pdf,
    b"q 1 0 0 1 50 420 cm /PSX Do Q\n"
    b"q 1 0 0 1 50 300 cm /FmGrp Do Q\n"
    b"BT /TT1 12 Tf 50 200 Td (AB) Tj ET\n",
)

pages = [
    (page1_content, Dictionary()),
    (page2_content, Dictionary(XObject=Dictionary(Im1=img_interp))),
    (
        page3_content,
        Dictionary(
            XObject=Dictionary(PSX=psx, FmGrp=group_form),
            Font=Dictionary(TT1=font),
        ),
    ),
]

for content, resources in pages:
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=content,
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# A conforming annotation on page 1: F=4, an appearance stream, no /C. It
# guards against a page-scoped annotation check reporting the wrong page.
pdf.pages[0].Annots = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Square,
                Rect=Array([400, 700, 500, 750]),
                F=4,
                AP=Dictionary(N=Stream(pdf, b"")),
            )
        )
    ]
)

# A /Movie annotation on page 3 only: ISO 19005-2, 6.3.1 t1 forbids the type
# and 6.3.3 t1 catches the absent appearance stream.
pdf.pages[2].Annots = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Movie,
                Rect=Array([400, 600, 500, 650]),
                F=4,
                T=String("clip"),
                Movie=Dictionary(F=String("clip.mov")),
            )
        )
    ]
)

# Deliberately no /Metadata on the catalog and no /OutputIntents, so
# ISO 19005-2, 6.6.2.1 t1 fires once at document level.
save(pdf, "multipage_2b.pdf", min_version="1.7")
