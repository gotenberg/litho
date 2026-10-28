#!/usr/bin/env python3
"""Build sharedk_ua1.pdf: a structure /K array two elements share.

ISO 32000-1, 14.7.2 makes the structure tree a strict tree, and Table 323 lets
an element's /K be an array, which ISO 32000-1, 7.3.10 lets a file write as an
object of its own. Two Sect elements here name one such array as their /K. It
holds a Figure and a Span written directly in it, so no object number marks
them: a walk that remembers only the elements it met, never the arrays it
read, reads the two again under the second Sect. Were those direct elements to
name the array again as their own /K, each level would double the work, which
is how a four-object file ran until the caller's timeout; this file shares the
array once, which every tool reads to the end.

The Figure has no alternate text and encloses nothing (ISO 14289-1, 7.3 t1;
ISO 14289-2, 8.2.5.28.2 t1). litho reads the shared array once and reports it
once, as veraPDF 1.30.2 does: the rule set and the instance count agree.

A P element beside the two Sects marks a filled rectangle, so the page has
tagged content and draws no text.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

content = b"/P <</MCID 0>> BDC\n0 g 50 700 100 20 re f\nEMC\n"
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Contents=Stream(pdf, content),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US"))
)
para = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0))
sect_a = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Sect, P=document))
sect_b = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Sect, P=document))

# The array both Sects name as their /K, an object of its own holding two
# elements written directly in it. Their /P names the first Sect.
shared = pdf.make_indirect(
    Array(
        [
            Dictionary(Type=Name.StructElem, S=Name.Figure, P=sect_a),
            Dictionary(Type=Name.StructElem, S=Name.Span, P=sect_a),
        ]
    )
)
sect_a.K = shared
sect_b.K = shared

document.K = Array([para, sect_a, sect_b])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array([para])])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "sharedk_ua1.pdf", min_version="1.7")
