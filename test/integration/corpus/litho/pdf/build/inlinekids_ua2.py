#!/usr/bin/env python3
"""Build inlinekids_ua2.pdf: Figures of their own that inline elements list.

ISO 32000-1, 14.7.2, Table 323 lets an element's /K hold a structure element
directly, with no object number of its own, and lets that inline element's /K
list elements that do have one. ISO 14289-2, 8.2.5.28.2 and ISO 14289-1, 7.3
ask each Figure for alternate text wherever it sits, and aggressive
remediation takes out one that encloses nothing: a Figure only an inline
element lists leaves that element's /K in the object holding it. No corpus
file lists an element of its own from an inline one, so this file packs the
places such a Figure sits into one page:

  * F1, the /K of a Div written inline in the Document's /K array, holds
    MCID 1, a drawing.
  * F2, in the /K array of a Part written inline, holds MCID 9, which the page
    does not mark: it encloses nothing.
  * F3, alone in a /K array written as an object of its own that an inline
    Sect names, has no kid.
  * F4, the /K of a Figure itself written inline, holds the null object: both
    enclose nothing, the cascade running through Figures.
  * F5, the /K of an inline NonStruct that also names it in /Ref (ISO
    32000-2, Table 355), has no kid.

Each inline holder has a structure type of its own: veraPDF gives every
element written inline the identity 0 0 obj and reads two inline elements of
one type as one, so two inline Divs would hide the first one's Figure from it.

A P marks MCID 0 around a filled rectangle. The page's /ParentTree array names
the P and F1.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"

content = (
    b"/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n"
    b"/Figure <</MCID 1>> BDC\n50 600 m 150 650 l S\nEMC\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Contents=Stream(pdf, content),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))
page = pdf.pages[0].obj

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, NS=ns20, P=struct_root)
)


def inline(s, k=None, **extra):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=document, NS=ns20, Pg=page, **extra)
    if k is not None:
        d.K = k
    return d


def figure(k=None):
    # An element's /P is an indirect reference (ISO 32000-1, Table 323), so
    # each Figure names the nearest element that has an object number.
    d = Dictionary(Type=Name.StructElem, S=Name.Figure, P=document, NS=ns20, Pg=page)
    if k is not None:
        d.K = k
    return pdf.make_indirect(d)


para = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)
f1 = figure(1)
f2 = figure(9)
f3 = figure()
f4 = figure(Array([None]))
f5 = figure()

document.K = Array(
    [
        para,
        inline("Div", f1),
        inline("Part", Array([f2])),
        inline("Sect", pdf.make_indirect(Array([f3]))),
        inline("Figure", f4),
        inline("NonStruct", f5, Ref=Array([f5])),
    ]
)
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array([para, f1])])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "inlinekids_ua2.pdf", min_version="2.0")
