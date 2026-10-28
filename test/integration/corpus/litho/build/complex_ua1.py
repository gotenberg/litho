#!/usr/bin/env python3
"""Build complex_ua1.pdf: a PDF/UA-1 fixture packing many structure defects.

The structure tree is deliberately well-formed enough to reach every rule:
a missing /StructTreeRoot would mask most of ISO 14289-1 clause 7, which is
exactly the masking this corpus exists to catch. Marked content, the parent
tree and the role map are all wired, and the defects are planted one per
construct.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    rgb_image,
    save,
    unembedded_truetype,
    xmp_packet,
)

pdf = pikepdf.new()

font = pdf.make_indirect(unembedded_truetype())
img = rgb_image(pdf)

content = b"""/H1 <</MCID 0>> BDC
BT /F1 18 Tf 50 720 Td (Heading One) Tj ET
EMC
/H3 <</MCID 1>> BDC
BT /F1 14 Tf 50 690 Td (Skipped To Three) Tj ET
EMC
/P <</MCID 2>> BDC
BT /F1 12 Tf 50 660 Td (Body text) Tj ET
EMC
/Figure <</MCID 3>> BDC
q 100 0 0 60 50 580 cm /Im1 Do Q
EMC
/MyType <</MCID 4>> BDC
BT /F1 12 Tf 50 550 Td (Custom type) Tj ET
EMC
BT /F1 12 Tf 50 520 Td (Untagged real content) Tj ET
q 1 0 0 RG 50 500 m 200 500 l S Q
"""

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        StructParents=0,
        Resources=Dictionary(
            Font=Dictionary(F1=font), XObject=Dictionary(Im1=img)
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))


def elem(stype, mcid, parent, extra=None):
    d = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + stype),
        P=parent,
        Pg=page,
        K=mcid,
    )
    if extra:
        for k, v in extra.items():
            setattr(d, k, v)
    return pdf.make_indirect(d)


struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)

h1 = elem("H1", 0, document)
h3 = elem("H3", 1, document)
para = elem("P", 2, document)
figure = elem("Figure", 3, document)  # no /Alt, no /ActualText
custom = elem("MyType", 4, document)

# Link structure element wrapping a link annotation carrying no /Contents.
link_annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([50, 470, 200, 490]),
        F=4,
        Border=Array([0, 0, 0]),
        A=Dictionary(Type=Name.Action, S=Name.URI, URI=String("https://example.org")),
        StructParent=1,
    )
)
link = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Link,
        P=document,
        Pg=page,
        K=Dictionary(Type=Name.OBJR, Obj=link_annot),
    )
)
pdf.pages[0].Annots = Array([link_annot])

document.K = Array([h1, h3, para, figure, custom, link])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([h1, h3, para, figure, custom]), 1, link]))
)
struct_root.ParentTreeNextKey = 2

# Circular role map: /MyType resolves to /Foo, which loops back through /Bar.
# ISO 14289-1, 7.1 requires every non-standard type to map to a standard one.
struct_root.RoleMap = Dictionary(
    MyType=Name("/Foo"), Foo=Name("/Bar"), Bar=Name("/Foo")
)

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
# Deliberately no /Lang on the catalog (ISO 14289-1, 7.2).
# /DisplayDocTitle absent from /ViewerPreferences (ISO 14289-1, 7.1).
pdf.Root.ViewerPreferences = Dictionary(FitWindow=True)

attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "complex_ua1.pdf")
