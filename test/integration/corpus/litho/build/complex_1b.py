#!/usr/bin/env python3
"""Build complex_1b.pdf: a PDF/A-1b fixture built around part-1 prohibitions.

ISO 19005-1 bans outright several constructs later parts allow: any
transparency, embedded files, optional content, and the LZWDecode filter.
Those sit alongside the profile-independent defects so a part-1 run has to
report all of them at once.

Two must-not-fire controls guard against over-suppression:
  * a Type3 glyph whose /Widths entry is skewed but which is never rendered,
  * a hidden annotation with no appearance stream, exempted by 6.5.3.

See ../README.md for the authoritative expected firing set.
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

t3a = type3_font(
    pdf,
    [("gA", 800, "0 0 700 700 re f"), ("gB", 800, "0 0 600 600 re f")],
    [500, 800],
)
t3b = type3_font(
    pdf,
    [("gC", 600, "0 0 500 500 re f"), ("gD", 900, "0 0 400 400 re f")],
    [600, 100],
)
tt = unembedded_truetype()

img = rgb_image(pdf, interpolate=True)

# Soft mask on an image: transparency, forbidden by ISO 19005-1, 6.4.
smask = Stream(pdf, bytes([0, 128, 200, 255]))
smask.Type = Name.XObject
smask.Subtype = Name.Image
smask.Width = 2
smask.Height = 2
smask.ColorSpace = Name.DeviceGray
smask.BitsPerComponent = 8
img.SMask = pdf.make_indirect(smask)

lzw_form = Stream(pdf, b"")
lzw_form.write(lzw_encode(b"0 0 1 rg 5 5 20 20 re f\n"), filter=Name.LZWDecode)
lzw_form.Type = Name.XObject
lzw_form.Subtype = Name.Form
lzw_form.BBox = Array([0, 0, 30, 30])
lzw_form.Resources = Dictionary()

# Transparency group form XObject.
group_form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
group_form.Type = Name.XObject
group_form.Subtype = Name.Form
group_form.BBox = Array([0, 0, 50, 50])
group_form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
group_form.Resources = Dictionary()

# Reference XObject: forbidden by ISO 19005-1, 6.2.6.
ref_form = Stream(pdf, b"0 0 10 10 re f\n")
ref_form.Type = Name.XObject
ref_form.Subtype = Name.Form
ref_form.BBox = Array([0, 0, 10, 10])
ref_form.Resources = Dictionary()
ref_form.Ref = Dictionary(
    F=Dictionary(Type=Name.Filespec, F=String("target.pdf")), Page=0
)

psxobj = Stream(pdf, b"%!PS\n")
psxobj.Type = Name.XObject
psxobj.Subtype = Name.PS

# Blend mode plus constant alpha: two independent transparency defects.
gs = pdf.make_indirect(
    Dictionary(Type=Name.ExtGState, BM=Name.Multiply, ca=0.5, CA=0.4)
)

content = b"""q
/BadIntent ri
/GS0 gs
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
q 1 0 0 1 250 300 cm /FmRef Do Q
q 1 0 0 1 350 300 cm /PSX Do Q
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
            Im1=img,
            Fm1=group_form,
            FmLZW=lzw_form,
            FmRef=ref_form,
            PSX=pdf.make_indirect(psxobj),
        ),
        ExtGState=Dictionary(GS0=gs),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

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
# Hidden annotation: ISO 19005-1, 6.5.3 requires Print set and Hidden clear,
# so the flags themselves are the defect here.
hidden = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 500, 500, 550]),
    F=2,
    C=Array([0, 1, 0]),
)
# Fully conforming annotation: correct flags, an appearance stream, no /C.
# Must stay silent while the three above fire, proving the annotation rules
# are evaluated per object rather than latched on the first failure.
ok_ap = Stream(pdf, b"0 0 20 20 re f\n")
ok_ap.Type = Name.XObject
ok_ap.Subtype = Name.Form
ok_ap.BBox = Array([0, 0, 20, 20])
ok_ap.Resources = Dictionary()
ok_annot = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 400, 420, 420]),
    F=4,
    AP=Dictionary(N=pdf.make_indirect(ok_ap)),
)
pdf.pages[0].Annots = Array(
    [
        pdf.make_indirect(movie),
        pdf.make_indirect(square),
        pdf.make_indirect(hidden),
        pdf.make_indirect(ok_annot),
    ]
)

pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('litho');")
)
pdf.Root.Names = Dictionary(
    EmbeddedFiles=Dictionary(
        Names=Array(
            [String("payload.txt"), filespec(pdf, "payload.txt", b"litho corpus\n")]
        )
    )
)
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(OCGs=Array([ocg]), D=Dictionary(Order=Array([])))

# Deliberately no /Metadata on the catalog and no /OutputIntents.
save(pdf, "complex_1b.pdf", min_version="1.4")
