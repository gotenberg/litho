#!/usr/bin/env python3
"""Build complex_4.pdf: a PDF/A-4 fixture built around part-4 specifics.

ISO 19005-4 is the PDF 2.0 part, and it draws its lines in places no earlier
part does: a pdfaid:rev of 2020, a document information dictionary that may
only carry ModDate, page-level output intents, associated files with a
mandatory AFRelationship, and outright bans on Requirements and PresSteps.
Every defect below is independent, so a rule that short-circuits shows up as
a missing class rather than a different verdict.

Four must-not-fire controls guard against over-suppression:
  * the header is pinned to %PDF-2.0, which 6.1.2 requires,
  * pdfaid:part is 4 and pdfaid:conformance is absent, both correct,
  * one file specification carries /AFRelationship next to one that does not,
  * a second image carries /Interpolate false next to one set true.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    filespec,
    pikepdf,
    rgb_image,
    save,
    unembedded_truetype,
    xmp_packet,
)

pdf = pikepdf.new()

tt = pdf.make_indirect(unembedded_truetype())

img_interp = rgb_image(pdf, interpolate=True)
img_ok = rgb_image(pdf)
img_ok.Interpolate = False

# Transfer function in a graphics state parameter dictionary: ISO 19005-4,
# 6.2.5 bans /TR outright and restricts /TR2 to /Default.
gs = pdf.make_indirect(
    Dictionary(Type=Name.ExtGState, TR=Name.Identity, TR2=Name.Identity)
)

# Reference XObject: ISO 19005-4, 6.2.8.2.
ref_form = Stream(pdf, b"0 0 10 10 re f\n")
ref_form.Type = Name.XObject
ref_form.Subtype = Name.Form
ref_form.BBox = Array([0, 0, 10, 10])
ref_form.Resources = Dictionary()
ref_form.Ref = Dictionary(
    F=Dictionary(Type=Name.Filespec, F=String("target.pdf")), Page=0
)

# Transparency group with no blending colour space. With no PDF/A output
# intent anywhere, ISO 19005-4, 6.2.9 requires one or the other.
group_form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
group_form.Type = Name.XObject
group_form.Subtype = Name.Form
group_form.BBox = Array([0, 0, 50, 50])
group_form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
group_form.Resources = Dictionary()

content = b"""q
/GS0 gs
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
0.5 g
50 620 100 10 re f
BT /TT1 12 Tf 50 590 Td (AB) Tj ET
q 100 0 0 60 50 500 cm /Im1 Do Q
q 100 0 0 60 200 500 cm /Im2 Do Q
q 1 0 0 1 50 400 cm /FmRef Do Q
q 1 0 0 1 200 400 cm /FmGrp Do Q
Q
"""

# Page-level output intents (ISO 32000-2, 14.11.5), a PDF 2.0 construct that
# earlier PDF/A parts have no place for. Neither entry carries a destination
# profile, so none of them is a PDF/A output intent and none of the ISO
# 19005-4, 6.2.3 tests has anything to reject: a firing here would be a false
# positive on a legal PDF 2.0 construct.
def output_intent(condition):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.OutputIntent,
            S=Name("/GTS_PDFA1"),
            OutputConditionIdentifier=String(condition),
            RegistryName=String("http://www.color.org"),
        )
    )


page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(TT1=tt),
        XObject=Dictionary(
            Im1=img_interp, Im2=img_ok, FmRef=ref_form, FmGrp=group_form
        ),
        ExtGState=Dictionary(GS0=gs),
    ),
    Contents=Stream(pdf, content),
    OutputIntents=Array([output_intent("CGATS TR 001"), output_intent("FOGRA39")]),
    # ISO 19005-4, 6.11 bans /PresSteps on a page.
    PresSteps=Dictionary(Type=Name.NavNode),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Movie is not in the ISO 32000-2 annotation table, so 6.3.1 rejects it.
movie = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Movie,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    T=String("clip"),
    Movie=Dictionary(F=String("clip.mov")),
)
# Control: correct flags, an appearance stream, an allowed subtype. Must stay
# silent while the Movie annotation fires.
ok_ap = Stream(pdf, b"0 0 20 20 re f\n")
ok_ap.Type = Name.XObject
ok_ap.Subtype = Name.Form
ok_ap.BBox = Array([0, 0, 20, 20])
ok_ap.Resources = Dictionary()
ok_annot = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 600, 420, 620]),
    F=4,
    AP=Dictionary(N=pdf.make_indirect(ok_ap)),
)
pdf.pages[0].Annots = Array([pdf.make_indirect(movie), pdf.make_indirect(ok_annot)])

# Launch is one of the actions ISO 19005-4, 6.6.1 still bans; JavaScript is
# not, which is a part-4 departure from parts 1 to 3.
pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.Launch, F=String("helper.exe")
)
pdf.Root.NeedsRendering = True
# ISO 19005-4, 6.12 bans /Requirements outright.
pdf.Root.Requirements = Array(
    [Dictionary(Type=Name.Requirement, S=Name("/EnableJavaScripts"))]
)

# Associated files. The catalog-level one is fully formed; the one in the
# /EmbeddedFiles name tree carries no /AFRelationship, which ISO 19005-4,
# 6.9 requires on every embedded file specification.
af_ok = filespec(
    pdf, "data.xml", b"<litho/>\n", relationship="/Data", subtype="/text#2Fxml"
)
af_bad = filespec(pdf, "payload.txt", b"litho corpus\n")
pdf.Root.AF = Array([af_ok])
pdf.Root.Names = Dictionary(
    EmbeddedFiles=Dictionary(Names=Array([String("payload.txt"), af_bad]))
)

# Optional content configuration with no /Name (ISO 19005-4, 6.10).
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(OCGs=Array([ocg]), D=Dictionary(Order=Array([])))

# A document information dictionary is allowed only when the catalog carries
# /PieceInfo, and then only with /ModDate (ISO 19005-4, 6.1.3). This one has
# neither, so both tests fire. /ModDate is left out on purpose: supplying it
# would drag in the separate docinfo-versus-XMP date consistency check and
# entangle two otherwise independent defects.
pdf.docinfo["/Title"] = String("litho corpus fixture")

# pdfaid:part is 4 (correct) but pdfaid:rev is 2019, which 6.7.3 rejects.
attach_xmp(pdf, xmp_packet(part=4, rev=2019))

save(pdf, "complex_4.pdf", min_version="2.0")
