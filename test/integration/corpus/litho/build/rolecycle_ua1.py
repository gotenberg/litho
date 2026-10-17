#!/usr/bin/env python3
"""Build rolecycle_ua1.pdf: standard structure types inside a /RoleMap cycle.

veraPDF 1.30.2 judges a circular mapping (ISO 14289-1, 7.1 test 6; ISO 19005-1,
6.8.3.4 test 2; ISO 19005-2 and -3, 6.7.3.4 test 2) per structure element: it
follows the root /RoleMap from the element's own type, by name, without
stopping on a standard type, and reports the element when the chain revisits a
type. Under a root `Document`, the /RoleMap sends `Sect` to `Book` and `Book`
back to `Sect`, and:

  * a `Sect` sits in the cycle: a standard type whose mapping returns to it;
  * an `Item` maps to `Sect`, so its chain enters the cycle without being in
    it, and stops on the standard `Sect` for every reading that stops there;
  * a `Div` nothing maps, the control.

The font is embedded and draws only `A` and `B`, which is all the text uses.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}

program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRoleCycle"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRoleCycle"),
                Flags=32,
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile2=pdf.make_indirect(font_file),
            )
        ),
    )
)


def marked(tag, mcid, y, s):
    return (
        b"/%s <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n"
        % (tag, mcid, y, s)
    )


content = (
    marked(b"H1", 0, 740, b"AABB")
    + marked(b"P", 1, 700, b"ABAB")
    + marked(b"P", 2, 660, b"BABA")
    + marked(b"P", 3, 620, b"BBAA")
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, content),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))
page = pdf.pages[0].obj

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)


def elem(s, parent, k):
    return pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent, K=k)
    )


def para(parent, mcid):
    p = elem("P", parent, mcid)
    p.Pg = page
    return p


title = elem("H1", document, 0)
title.Pg = page
sect = elem("Sect", document, Array())
item = elem("Item", document, Array())
div = elem("Div", document, Array())
paras = [para(sect, 1), para(item, 2), para(div, 3)]
sect.K = Array([paras[0]])
item.K = Array([paras[1]])
div.K = Array([paras[2]])
document.K = Array([title, sect, item, div])

struct_root.K = Array([document])
struct_root.RoleMap = Dictionary(Sect=Name("/Book"), Book=Name.Sect, Item=Name.Sect)
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([title] + paras)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "rolecycle_ua1.pdf")
