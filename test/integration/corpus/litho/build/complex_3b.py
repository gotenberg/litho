#!/usr/bin/env python3
"""Build complex_3b.pdf: PDF/A-3b, embedded and associated files.

Part 3 is the part that lets a conforming file carry arbitrary attachments,
and it pays for that with four requirements on every embedded file
specification: a real MIME type, both name keys, a declared relationship,
and membership of an /AF array. ISO 19005-3 6.8 states them as four
independent tests, so this fixture breaks each one in a separate file
specification and leaves a fifth one fully conformant.

Four must-not-fire controls guard against over-suppression:
  * a fully conformant associated file, which no 6.8 test may touch,
  * each defective file specification is correct in every respect except
    the one it is there to break, so a rule that latches on the first bad
    specification reports the wrong count,
  * a conforming annotation with correct flags and an appearance stream.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    lzw_encode,
    pikepdf,
    rgb_image,
    save,
    unembedded_truetype,
    xmp_packet,
)

pdf = pikepdf.new()


def attachment(name, payload, mime="/text#2Fplain", uf=True, relationship="/Data"):
    """Build one embedded-file specification, defective in at most one way."""
    ef = Stream(pdf, payload)
    ef.Type = Name.EmbeddedFile
    if mime is not None:
        ef.Subtype = Name(mime)
    ef.Params = Dictionary(Size=len(payload))
    spec = Dictionary(
        Type=Name.Filespec,
        F=String(name),
        EF=Dictionary(F=pdf.make_indirect(ef)),
        Desc=String("litho corpus attachment"),
    )
    if uf:
        spec.UF = String(name)
    if relationship is not None:
        spec.AFRelationship = Name(relationship)
    return pdf.make_indirect(spec)


# One defect per specification, so the four ISO 19005-3 6.8 tests stay
# independent of one another.
no_mime = attachment("no-mime.bin", b"litho corpus\n", mime=None)  # t1
no_uf = attachment("no-uf.txt", b"litho corpus\n", uf=False)  # t2
no_rel = attachment("no-rel.txt", b"litho corpus\n", relationship=None)  # t3
not_associated = attachment("orphan.txt", b"litho corpus\n")  # t4
# Control: correct in every respect, and reachable from /AF. No 6.8 test may
# fire on it, so a firing is a false positive that the four above would hide.
conformant = attachment("good.txt", b"litho corpus\n")

tt = unembedded_truetype()
img = rgb_image(pdf, interpolate=True)

lzw_form = Stream(pdf, b"")
lzw_form.write(lzw_encode(b"0 0 1 rg 5 5 20 20 re f\n"), filter=Name.LZWDecode)
lzw_form.Type = Name.XObject
lzw_form.Subtype = Name.Form
lzw_form.BBox = Array([0, 0, 30, 30])
lzw_form.Resources = Dictionary()

group_form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
group_form.Type = Name.XObject
group_form.Subtype = Name.Form
group_form.BBox = Array([0, 0, 50, 50])
group_form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
group_form.Resources = Dictionary()

psxobj = Stream(pdf, b"%!PS\n")
psxobj.Type = Name.XObject
psxobj.Subtype = Name.PS

content = b"""q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 660 100 30 re f
0.5 g
50 640 100 10 re f
BT /TT1 12 Tf 50 610 Td (AB) Tj ET
q 100 0 0 60 50 520 cm /Im1 Do Q
q 1 0 0 1 50 430 cm /Fm1 Do Q
q 1 0 0 1 150 430 cm /FmLZW Do Q
q 1 0 0 1 250 430 cm /PSX Do Q
Q
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(TT1=pdf.make_indirect(tt)),
        XObject=Dictionary(
            Im1=img,
            Fm1=group_form,
            FmLZW=lzw_form,
            PSX=pdf.make_indirect(psxobj),
        ),
    ),
    Contents=Stream(pdf, content),
    # A page-level /AF array, which ISO 32000-2 14.13 allows and part 3
    # accepts: the specification it points at is fully conformant.
    AF=Array([conformant]),
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
hidden = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 600, 500, 650]),
    F=2,
    C=Array([0, 1, 0]),
)
ok_ap = Stream(pdf, b"0 0 20 20 re f\n")
ok_ap.Type = Name.XObject
ok_ap.Subtype = Name.Form
ok_ap.BBox = Array([0, 0, 20, 20])
ok_ap.Resources = Dictionary()
ok_annot = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 500, 420, 520]),
    F=4,
    AP=Dictionary(N=pdf.make_indirect(ok_ap)),
)
pdf.pages[0].Annots = Array(
    [pdf.make_indirect(movie), pdf.make_indirect(hidden), pdf.make_indirect(ok_annot)]
)

# Everything except orphan.txt is an associated file. orphan.txt is reachable
# only through the /EmbeddedFiles name tree, which is exactly what 6.8 t4
# rejects.
pdf.Root.AF = Array([no_mime, no_uf, no_rel])
pdf.Root.Names = Dictionary(
    EmbeddedFiles=Dictionary(
        Names=Array(
            [
                String("good.txt"),
                conformant,
                String("no-mime.bin"),
                no_mime,
                String("no-rel.txt"),
                no_rel,
                String("no-uf.txt"),
                no_uf,
                String("orphan.txt"),
                not_associated,
            ]
        )
    )
)

pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('litho');")
)
pdf.Root.NeedsRendering = True
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg]), D=Dictionary(Order=Array([]), AS=Array([]))
)

# The conformance claim is correct: part 3, level B. The defects are in the
# attachments, not in the claim.
attach_xmp(pdf, xmp_packet(part=3, conformance="B"))

save(pdf, "complex_3b.pdf")
