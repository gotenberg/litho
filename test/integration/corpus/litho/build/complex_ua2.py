#!/usr/bin/env python3
"""Build complex_ua2.pdf: a PDF/UA-2 fixture built around part-2 specifics.

ISO 14289-2 moves the goalposts from ISO 14289-1 in ways no single-rule
corpus file exercises together: PDF 2.0 structure namespaces, a pdfuaid:rev
of 2024 alongside pdfuaid:part 2, Unicode Private Use Area handling, and
structure destinations. Every defect below is independent of the others, so
a rule that short-circuits shows up as a missing class rather than a
different verdict.

Four must-not-fire controls guard against over-suppression:
  * a Figure carrying /Alt next to one that carries none,
  * a struct element with a well-formed /Lang next to one with a bad tag,
  * a Link annotation properly enclosed in a Link structure element,
  * an invisible annotation outside the structure tree, which 8.9.2.2
    exempts because it has no enclosing structure element.

See ../README.md for the authoritative expected firing set.
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
    tounicode_cmap,
    unembedded_truetype,
    xmp_packet,
)

PUA = ""

pdf = pikepdf.new()

# F1 renders ordinary text and carries no font program (ISO 14289-2, 8.4.5.5.1).
f1 = pdf.make_indirect(unembedded_truetype())

# F2 maps code 65 onto a Unicode PUA value. ISO 14289-2, 8.4.3 then demands an
# /Alt or /ActualText on the enclosing structure element, and the P below has
# neither.
f2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
        FirstChar=65,
        LastChar=65,
        Widths=Array([722]),
        ToUnicode=tounicode_cmap(pdf, {65: 0xE000}),
    )
)

img = rgb_image(pdf)

content = b"""/H1 <</MCID 0>> BDC
BT /F1 18 Tf 50 720 Td (Heading One) Tj ET
EMC
/P <</MCID 1>> BDC
BT /F1 12 Tf 50 690 Td (Body text with a bad language tag) Tj ET
EMC
/P <</MCID 2>> BDC
BT /F1 12 Tf 50 665 Td (Body text with a good language tag) Tj ET
EMC
/Figure <</MCID 3>> BDC
q 80 0 0 50 50 600 cm /Im1 Do Q
EMC
/Figure <</MCID 4>> BDC
q 80 0 0 50 150 600 cm /Im1 Do Q
EMC
/P <</MCID 5>> BDC
BT /F2 12 Tf 50 570 Td (A) Tj ET
EMC
/Alpha <</MCID 6>> BDC
BT /F1 12 Tf 50 545 Td (Namespaced custom type) Tj ET
EMC
/MyType <</MCID 7>> BDC
BT /F1 12 Tf 50 520 Td (Legacy custom type) Tj ET
EMC
/Gamma <</MCID 8>> BDC
BT /F1 12 Tf 50 495 Td (Unmapped namespaced type) Tj ET
EMC
/P <</MCID 9>> BDC
BT /F1 12 Tf 50 470 Td (Orphan element, no parent entry) Tj ET
EMC
/Link <</MCID 10>> BDC
BT /F1 12 Tf 50 445 Td (Jump to a page destination) Tj ET
EMC
BT /F1 12 Tf 50 415 Td (Untagged real content) Tj ET
q 1 0 0 RG 50 400 m 250 400 l S Q
"""

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        StructParents=0,
        Resources=Dictionary(
            Font=Dictionary(F1=f1, F2=f2), XObject=Dictionary(Im1=img)
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# PDF 2.0 structure namespaces (ISO 32000-2, 14.7.4). The standard structure
# namespace is what 8.2.5.2/2 wants on the Document element; the custom one
# carries a /RoleMapNS entry that maps one of its own types onto another type
# in the same namespace, which 8.2.4/3 forbids.
ssn = pdf.make_indirect(
    Dictionary(Type=Name.Namespace, NS=String("http://iso.org/pdf2/ssn"))
)
custom_ns = pdf.make_indirect(
    Dictionary(Type=Name.Namespace, NS=String("http://litho.example/ns"))
)
custom_ns.RoleMapNS = Dictionary(Alpha=Array([Name("/Beta"), custom_ns]))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ssn,
        Lang=String("en-US"),
    )
)


def elem(stype, mcid, extra=None, parent=document):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + stype), Pg=page, K=mcid)
    if parent is not None:
        d.P = parent
    if extra:
        for k, v in extra.items():
            setattr(d, k, v)
    return pdf.make_indirect(d)


h1 = elem("H1", 0)
bad_lang = elem("P", 1, {"Lang": String("english!")})
good_lang = elem("P", 2, {"Lang": String("en-US")})
figure_bad = elem("Figure", 3)
figure_ok = elem("Figure", 4, {"Alt": String("A red, green and blue swatch")})
pua_para = elem("P", 5)
alpha = elem("Alpha", 6, {"NS": custom_ns})
mytype = elem("MyType", 7)
# Gamma sits in the same non-standard namespace but carries no /RoleMapNS
# entry at all, so it is unambiguously unmapped for ISO 14289-2, 8.2.4 t1.
gamma = elem("Gamma", 8, {"NS": custom_ns})
# ISO 14289-2, 8.2.1 requires the /P entry on every structure element.
orphan = elem("P", 9, parent=None)

# Link annotation reached through a GoTo action whose destination is an
# explicit page destination rather than a structure destination
# (ISO 14289-2, 8.8). Its /Contents carries a PUA code point (8.6).
link_annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([50, 440, 250, 460]),
        F=4,
        Border=Array([0, 0, 0]),
        Contents=String("Jump " + PUA),
        A=Dictionary(
            Type=Name.Action,
            S=Name.GoTo,
            D=Array([page, Name.XYZ, 0, 792, 0]),
        ),
        StructParent=1,
    )
)
link = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Link,
        P=document,
        Pg=page,
        K=Array([10, Dictionary(Type=Name.OBJR, Obj=link_annot)]),
    )
)

# Invisible annotation inside the structure tree: ISO 14289-2, 8.9.2.2
# requires it to be an artifact.
invisible = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([400, 700, 500, 750]),
        F=1,
        Contents=String("An invisible square"),
        StructParent=2,
    )
)
invisible_elem = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Annot,
        P=document,
        Pg=page,
        K=Dictionary(Type=Name.OBJR, Obj=invisible),
    )
)

# Control: the same Invisible flag on an annotation with no enclosing
# structure element. 8.9.2.2 exempts it, so a firing here is a false
# positive that would hide the scoping of the rule above.
ghost = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([400, 600, 500, 650]),
        F=1,
        Border=Array([0, 0, 0]),
    )
)

pdf.pages[0].Annots = Array([link_annot, invisible, ghost])
# Deliberately no /Tabs on the page (ISO 14289-2, 8.9.3.3).

mcid_owners = [
    h1,
    bad_lang,
    good_lang,
    figure_bad,
    figure_ok,
    pua_para,
    alpha,
    mytype,
    gamma,
    orphan,
    link,
]
document.K = Array(mcid_owners + [invisible_elem])
struct_root.K = Array([document])
struct_root.Namespaces = Array([ssn, custom_ns])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array([0, Array(mcid_owners), 1, link, 2, invisible_elem])
    )
)
struct_root.ParentTreeNextKey = 3

# Circular role map for the legacy (PDF 1.7 namespace) custom type:
# /MyType resolves to /Foo, which loops back through /Bar.
struct_root.RoleMap = Dictionary(
    MyType=Name("/Foo"), Foo=Name("/Bar"), Bar=Name("/Foo")
)

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
# /MarkInfo is deliberately correct: ISO 14289-2, 6.2 is the gate that every
# clause-8 rule sits behind, so breaking it here would mask the whole fixture.

# Optional content configurations (ISO 14289-2, 8.7). The default config
# carries an /AS array, which t2 forbids outright. t1 wants a non-empty
# /Name on every configuration, but veraPDF only reaches it when an
# alternate /Configs array exists, so one is supplied.
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg]),
    D=Dictionary(Order=Array([]), AS=Array([])),
    Configs=Array([Dictionary(Order=Array([]))]),
)

# Deliberately no /Lang on the catalog (ISO 14289-2, 8.4.4) and no
# /ViewerPreferences, so /DisplayDocTitle is absent (8.11.2).

# pdfuaid:part is 1 and pdfuaid:rev is 2023: two independent clause-5
# defects in one packet. Catching both at once is what exposed the
# early-return masking fixed in d0db11d.
attach_xmp(pdf, xmp_packet(ua_part=1, ua_rev=2023))

save(pdf, "complex_ua2.pdf", min_version="2.0")
