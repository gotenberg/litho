#!/usr/bin/env python3
"""Build complex_2a.pdf: PDF/A-2a, tagging defects layered on part-2 rules.

Conformance level A adds a logical structure hierarchy, a role map that has
to resolve, and language tags that have to parse. Those live in ISO 19005-2
6.7.3 and 6.7.4 and sit on top of every part-2 rule the file already breaks,
so a level-A run has to report both layers at once.

Three must-not-fire controls guard against over-suppression:
  * /MarkInfo /Marked true and a wired /StructTreeRoot, the gate every
    level-A structure rule sits behind,
  * a standard structure type left alone next to one that is remapped,
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

tt = pdf.make_indirect(unembedded_truetype())
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
/P <</MCID 0>> BDC
1 0 0 rg
50 700 100 50 re f
BT /TT1 12 Tf 50 680 Td (Body text) Tj ET
EMC
/Div <</MCID 1>> BDC
0 0 0 1 k
50 640 100 50 re f
EMC
/MyType <</MCID 2>> BDC
0.5 g
50 610 100 10 re f
EMC
/Unmapped <</MCID 3>> BDC
q 100 0 0 60 50 520 cm /Im1 Do Q
EMC
/P <</MCID 4>> BDC
q 1 0 0 1 50 430 cm /Fm1 Do Q
q 1 0 0 1 150 430 cm /FmLZW Do Q
q 1 0 0 1 250 430 cm /PSX Do Q
EMC
Q
"""

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        StructParents=0,
        Resources=Dictionary(
            Font=Dictionary(TT1=tt),
            XObject=Dictionary(
                Im1=img,
                Fm1=group_form,
                FmLZW=lzw_form,
                PSX=pdf.make_indirect(psxobj),
            ),
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)


def elem(stype, mcid, extra=None):
    d = Dictionary(
        Type=Name.StructElem, S=Name("/" + stype), P=document, Pg=page, K=mcid
    )
    if extra:
        for k, v in extra.items():
            setattr(d, k, v)
    return pdf.make_indirect(d)


para = elem("P", 0)
div = elem("Div", 1)
mytype = elem("MyType", 2)
unmapped = elem("Unmapped", 3)
# A structure element whose own /Lang fails the Language-Tag grammar of
# ISO 19005-2, 6.7.4. The catalog carries a well-formed /Lang, so the
# grammar check has to run per object rather than once per document.
bad_lang = elem("P", 4, {"Lang": String("english!")})

mcid_owners = [para, div, mytype, unmapped, bad_lang]

# Role map defects, one per ISO 19005-2, 6.7.3.4 test:
#   t2  /MyType resolves to /Foo, which loops back through /Bar.
#   t3  /Div is a standard type remapped to a non-standard one.
#   t1  /Unmapped appears in the tree and in no mapping at all.
# /Document is deliberately left out of the map: a standard type with no
# entry must stay silent, so a firing on it would be a false positive.
struct_root.RoleMap = Dictionary(
    MyType=Name("/Foo"),
    Foo=Name("/Bar"),
    Bar=Name("/Foo"),
    Div=Name("/Custom"),
)

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")

movie = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Movie,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    T=String("clip"),
    Movie=Dictionary(F=String("clip.mov")),
)
# Hidden flag set and Print clear: the flags are the defect (6.3.2).
hidden = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 600, 500, 650]),
    F=2,
    C=Array([0, 1, 0]),
    StructParent=1,
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
    StructParent=2,
)
hidden_ref = pdf.make_indirect(hidden)
ok_ref = pdf.make_indirect(ok_annot)
pdf.pages[0].Annots = Array([pdf.make_indirect(movie), hidden_ref, ok_ref])

# Both Square annotations are markup annotations, so each is wrapped in its
# own /Annot structure element and registered in the parent tree. That keeps
# the tagging of the annotations conformant and isolates the annotation
# defects (flags, missing appearance stream) from the structure defects.
annot_elems = [
    pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Annot,
            P=document,
            Pg=page,
            K=Dictionary(Type=Name.OBJR, Obj=ref),
        )
    )
    for ref in (hidden_ref, ok_ref)
]

document.K = Array(mcid_owners + annot_elems)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array([0, Array(mcid_owners), 1, annot_elems[0], 2, annot_elems[1]])
    )
)
struct_root.ParentTreeNextKey = 3

pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('litho');")
)
pdf.Root.NeedsRendering = True
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg]), D=Dictionary(Order=Array([]), AS=Array([]))
)

# The conformance claim itself is correct: part 2, level A. The defects are
# in the file, not in the claim, so a validator cannot take the shortcut of
# rejecting the claim and stopping there.
attach_xmp(pdf, xmp_packet(part=2, conformance="A"))

save(pdf, "complex_2a.pdf")
