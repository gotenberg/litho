#!/usr/bin/env python3
"""Build treekids_ua1.pdf: structure parent and ID trees whose /Kids arrays
are objects of their own, and text past the crop box, under PDF/UA-1.

ISO 32000-1, 7.9.6 and 7.9.7 make a name or number tree's /Kids an array of
indirect references to its nodes; nothing stops the array itself being written
as an object of its own and named by reference. Here the /ParentTree (ISO
32000-1, 14.7.4.4) and the /IDTree (Table 322) each hold their one leaf
directly in such an array. A walk that marks only the nodes it met, never the
arrays it read, reads that array again wherever a direct node names it, and a
file whose direct nodes each name their own array again doubles the work per
level. This file names each array once, which every tool reads to the end: it
gives the split, merge and flatten rewrites, which read both trees whole to
carry the structure, the shape to read.

| Tree        | Root                | /Kids                  | Leaf, written directly in /Kids |
| ----------- | ------------------- | ---------------------- | ------------------------------- |
| /ParentTree | an object, no /Nums | an array, by reference | /Nums for keys 0 and 1          |
| /IDTree     | an object, no /Names| an array, by reference | /Names for p1, p2 and p3        |

Page 1 inherits its /MediaBox [0 0 612 792] from the page tree node and states
/CropBox [0 0 300 792]. Its second paragraph is drawn at x = 400, past the crop
box: a viewer shows the crop box and hides it, a reader that ignores the crop
box shows it. A redaction of page 1 clears it all the same, since it answers
for every reader of the file. Page 2 draws one paragraph.

The font is Helvetica, not embedded (ISO 14289-1, 7.21.4.1 t1), and the XMP
packet carries no dc:title (7.1 t9): both are independent of the trees.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def para(mcid, x, y, s):
    return b"/P <</MCID %d>> BDC\nBT /F1 12 Tf %d %d Td (%s) Tj ET\nEMC\n" % (mcid, x, y, s)


pdf.Root.Pages.MediaBox = Array([0, 0, 612, 792])
resources = Dictionary(Font=Dictionary(F1=font))
page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        CropBox=Array([0, 0, 300, 792]),
        Resources=resources,
        Contents=pdf.make_indirect(
            Stream(pdf, para(0, 50, 700, b"inside the crop box") + para(1, 400, 700, b"past the crop box"))
        ),
        StructParents=0,
    )
)
page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        Resources=resources,
        Contents=pdf.make_indirect(Stream(pdf, para(0, 50, 700, b"page two"))),
        StructParents=1,
    )
)
pdf.Root.Pages.Kids = Array([page1, page2])
pdf.Root.Pages.Count = 2

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US"))
)
p1 = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page1, K=0, ID=String("p1"))
)
p2 = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page1, K=1, ID=String("p2"))
)
p3 = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page2, K=0, ID=String("p3"))
)
document.K = Array([p1, p2, p3])
struct_root.K = Array([document])

parent_kids = pdf.make_indirect(
    Array([Dictionary(Limits=Array([0, 1]), Nums=Array([0, Array([p1, p2]), 1, Array([p3])]))])
)
struct_root.ParentTree = pdf.make_indirect(Dictionary(Kids=parent_kids))
struct_root.ParentTreeNextKey = 2

id_kids = pdf.make_indirect(
    Array(
        [
            Dictionary(
                Limits=Array([String("p1"), String("p3")]),
                Names=Array([String("p1"), p1, String("p2"), p2, String("p3"), p3]),
            )
        ]
    )
)
struct_root.IDTree = pdf.make_indirect(Dictionary(Kids=id_kids))

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "treekids_ua1.pdf", min_version="1.7")
