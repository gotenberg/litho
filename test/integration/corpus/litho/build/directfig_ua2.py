#!/usr/bin/env python3
"""Build directfig_ua2.pdf: Figures written directly in a structure /K.

ISO 32000-1, 14.7.2, Table 323 lets an element's /K hold a structure element
directly, with no object number of its own. ISO 14289-2, 8.2.5.28.2 and ISO
14289-1, 7.3 ask such a Figure for alternate text like any other, so
remediation writes it into the object holding the Figure, and aggressive
remediation takes out one that encloses nothing. No corpus file writes a
Figure directly, so this file packs the places one can sit into one page:

  * D1, directly in the Document's /K array, holds MCID 1, a drawing.
  * D2, directly in the /K array of a Div itself written inline in the
    Document's /K array, holds MCID 2, a drawing.
  * D3, directly in a /K array written as an object of its own that a Sect
    names, holds MCID 3, a drawing.
  * D4, directly in the Document's /K array, holds MCID 9, which the page
    does not mark: it encloses nothing.
  * D5, in the Sect's array beside D3, holds D6 alone, and D6 the null object:
    both enclose nothing, the cascade running through Figures.

A P marks MCID 0 around a filled rectangle. The page's /ParentTree array names,
for each identifier, the nearest element written as an object of its own.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"

content = (
    b"/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n"
    b"/Figure <</MCID 1>> BDC\n50 600 m 150 650 l S\nEMC\n"
    b"/Figure <</MCID 2>> BDC\n50 500 m 150 550 l S\nEMC\n"
    b"/Figure <</MCID 3>> BDC\n50 400 m 150 450 l S\nEMC\n"
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


def direct(s, parent, k=None):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, NS=ns20, Pg=page)
    if k is not None:
        d.K = k
    return d


para = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)
sect = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Sect, P=document, NS=ns20))
sect.K = pdf.make_indirect(
    Array(
        [
            direct("Figure", sect, 3),
            direct("Figure", sect, Array([direct("Figure", sect, Array([None]))])),
        ]
    )
)

document.K = Array(
    [
        para,
        direct("Figure", document, 1),
        direct("Div", document, Array([direct("Figure", document, 2)])),
        sect,
        direct("Figure", document, 9),
    ]
)
struct_root.K = Array([document])
struct_root.Namespaces = Array([ns20])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([para, document, document, sect])]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=2, ua_rev=2024))

save(pdf, "directfig_ua2.pdf", min_version="2.0")
