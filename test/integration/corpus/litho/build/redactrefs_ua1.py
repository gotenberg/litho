#!/usr/bin/env python3
"""Build redactrefs_ua1.pdf: copies of page text behind objects litho cannot read.

Redaction clears the copies of removed text the file holds elsewhere, and two
of those copies sit behind an object the scrub cannot read:

* The structure element describing page 1's paragraph holds its words in
  /ActualText. The page's /ParentTree entry names no element for the
  paragraph's MCID, its slot being null, but the element is in the tree,
  reached from the root's /K with /Pg naming page 1 (ISO 32000-1, 14.7.4).
* The outline's /First is the integer 7 rather than a reference to an outline
  item (ISO 32000-1, Table 153), so the chain from /First ends at once, while
  its /Last names the item that quotes the paragraph, whose /Prev is that
  integer again.

A rewrite copies both copies, so redacting page 1 has to clear them. Before,
the redaction refused: the null slot read as an element written directly into
the array, and a number in /First as an outline item it could not rewrite.

Page 2 draws text no redaction of page 1 touches, a control for the pages the
redaction leaves alone.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)
resources = Dictionary(Font=Dictionary(F1=font))

WORDS = b"the secret plan"

page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(
            pdf, b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 72 700 Td (" + WORDS + b") Tj ET\nEMC\n"
        ),
        StructParents=0,
    )
)
page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(pdf, b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 72 700 Td (page two) Tj ET\nEMC\n"),
        StructParents=1,
    )
)
pdf.pages.append(pikepdf.Page(page1))
pdf.pages.append(pikepdf.Page(page2))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US"))
)
secret = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.P,
        P=document,
        Pg=page1,
        K=0,
        ActualText=String(WORDS.decode()),
    )
)
other = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page2, K=0)
)
document.K = Array([secret, other])
struct_root.K = Array([document])
# The null slot: the /ParentTree names no element for page 1's MCID 0.
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([None]), 1, Array([other])]))
)
struct_root.ParentTreeNextKey = 2

# /First is a number, which is no outline item.
not_an_item = 7
outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines, Count=2))
quoting = pdf.make_indirect(
    Dictionary(
        Title=String(WORDS.decode()),
        Parent=outlines,
        Prev=not_an_item,
        Dest=Array([page1, Name.Fit]),
    )
)
outlines.First = not_an_item
outlines.Last = quoting

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.Outlines = outlines

save(pdf, "redactrefs_ua1.pdf", min_version="1.7")
