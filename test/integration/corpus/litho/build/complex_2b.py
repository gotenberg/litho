#!/usr/bin/env python3
"""Build complex_2b.pdf: a PDF/A-2b fixture packing many independent violations.

Rule of this corpus: a firing rule must never hide a rule that should also
fire. Each construct below is independent of the others, and two of them
are deliberate MUST-NOT-FIRE controls guarding against over-suppression in
litho's usage-scoping logic.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    filespec,
    lzw_encode,
    pikepdf,
    rgb_image,
    save,
    type3_font,
    unembedded_truetype,
)

pdf = pikepdf.new()

# Type3 T3A: code 65 is rendered and its /Widths entry (500) contradicts the
# CharProc d0 advance (800). Real mismatch, must fire.
t3a = type3_font(
    pdf,
    [("gA", 800, "0 0 700 700 re f"), ("gB", 800, "0 0 600 600 re f")],
    [500, 800],
)

# Type3 T3B: code 66 is never rendered, so its skewed /Widths entry (100 vs a
# d0 advance of 900) must stay silent. Guards the usage-scoping fix in
# c55de28 against over-suppression: code 65 of the same font is consistent,
# so a firing here would be a false positive.
t3b = type3_font(
    pdf,
    [("gC", 600, "0 0 500 500 re f"), ("gD", 900, "0 0 400 400 re f")],
    [600, 100],
)

tt = unembedded_truetype()

img = rgb_image(pdf, interpolate=True)

# LZWDecode is forbidden outright (ISO 19005-2, 6.1.7). The payload really is
# LZW so the validator must reject the filter name, not a decode failure.
# Stream.write with an explicit filter installs the pre-encoded bytes; the
# plain Stream constructor would let pikepdf transcode them to Flate on save.
lzw_payload = b"0 0 1 rg 5 5 20 20 re f\n"
lzw_form = Stream(pdf, b"")
lzw_form.write(lzw_encode(lzw_payload), filter=Name.LZWDecode)
lzw_form.Type = Name.XObject
lzw_form.Subtype = Name.Form
lzw_form.BBox = Array([0, 0, 30, 30])
lzw_form.Resources = Dictionary()

# Transparency group form XObject with no blending colour space.
form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 50, 50])
form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
form.Resources = Dictionary()

# PostScript XObject: forbidden by ISO 19005-2, 6.2.7.
psxobj = Stream(pdf, b"%!PS\n")
psxobj.Type = Name.XObject
psxobj.Subtype = Name.PS

content = b"""q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
0.5 g
50 620 100 10 re f
BT /T3A 12 Tf 50 600 Td (AB) Tj ET
BT /T3B 12 Tf 50 570 Td (A) Tj ET
BT /TT1 12 Tf 50 540 Td (AB) Tj ET
q 200 0 0 100 50 400 cm /Im1 Do Q
q 1 0 0 1 50 300 cm /Fm1 Do Q
q 1 0 0 1 150 300 cm /FmLZW Do Q
q 1 0 0 1 250 300 cm /PSX Do Q
Q
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(
            T3A=pdf.make_indirect(t3a),
            T3B=pdf.make_indirect(t3b),
            TT1=pdf.make_indirect(tt),
        ),
        XObject=Dictionary(
            Im1=img, Fm1=form, FmLZW=lzw_form, PSX=pdf.make_indirect(psxobj)
        ),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Forbidden annotation type plus an annotation with no appearance stream.
movie = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Movie,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    T=String("clip"),
    Movie=Dictionary(F=String("clip.mov")),
)
square = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 600, 500, 650]),
    F=4,
    C=Array([1, 0, 0]),
)
# Hidden annotation: ISO 19005-2, 6.3.2 requires Print set and Hidden clear,
# so the flags are the defect. It still owes an appearance stream.
hidden = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 500, 500, 550]),
    F=2,
    C=Array([0, 1, 0]),
)
# Zero-area annotation with no appearance stream. ISO 19005-2, 6.3.3 exempts
# annotations whose Rect collapses to a point, so this must stay silent while
# the three above fire. Guards the exemption against under-scoping.
zero_area = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 300, 400, 300]),
    F=4,
)
pdf.pages[0].Annots = Array(
    [
        pdf.make_indirect(movie),
        pdf.make_indirect(square),
        pdf.make_indirect(hidden),
        pdf.make_indirect(zero_area),
    ]
)

# Catalog-level violations: a JavaScript OpenAction, /NeedsRendering, an
# embedded file, and an optional content group with no /Name.
pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('litho');")
)
pdf.Root.NeedsRendering = True
pdf.Root.Names = Dictionary(
    EmbeddedFiles=Dictionary(
        Names=Array(
            [String("payload.txt"), filespec(pdf, "payload.txt", b"litho corpus\n")]
        )
    )
)
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg]), D=Dictionary(Order=Array([]), AS=Array([]))
)

# Deliberately no /Metadata on the catalog and no /OutputIntents.
save(pdf, "complex_2b.pdf")
