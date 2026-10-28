#!/usr/bin/env python3
"""Build tables_ua1.pdf: a PDF/UA-1 fixture built around tables and lists.

No other fixture in this corpus contains a /Table or an /L, so the table
rules of ISO 14289-1, 7.5 and the deeply nested table and list rows of the
ISO/TS 32005 Table 5 matrix are unreached by the rest of the corpus. Both are
nested structures, which is where a check that stops at the first defect, or
one that never descends past the outer element, hides a second finding.

The file holds two tables. The first is defective and the second conforms, so
a check that reports per document rather than per element is visible as a
count, and a check that latches on the first table is visible as a missing
rule on the second.

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
    save,
    unembedded_truetype,
    xmp_packet,
)

pdf = pikepdf.new()

font = pdf.make_indirect(unembedded_truetype())

# Marked content: two table cells, two list items, one paragraph. Each shows
# text so every element is a text item for the language rules.
content = b"""/TH <</MCID 0>> BDC
BT /F1 12 Tf 50 720 Td (Header) Tj ET
EMC
/TD <</MCID 1>> BDC
BT /F1 12 Tf 150 720 Td (Cell) Tj ET
EMC
/TH <</MCID 2>> BDC
BT /F1 12 Tf 50 680 Td (Header2) Tj ET
EMC
/TD <</MCID 3>> BDC
BT /F1 12 Tf 150 680 Td (Cell2) Tj ET
EMC
/LBody <</MCID 4>> BDC
BT /F1 12 Tf 50 640 Td (First item) Tj ET
EMC
/LBody <</MCID 5>> BDC
BT /F1 12 Tf 50 620 Td (Second item) Tj ET
EMC
/P <</MCID 6>> BDC
BT /F1 12 Tf 50 580 Td (Trailing paragraph) Tj ET
EMC
"""

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, content),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))


def elem(stype, parent, kids=None, extra=None):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + stype), P=parent, Pg=page)
    if kids is not None:
        d.K = kids
    if extra:
        for k, v in extra.items():
            setattr(d, k, v)
    return pdf.make_indirect(d)


struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US"))
)

# Table one is defective: the TH carries no /Scope and no /ID, and the TD
# carries no /Headers, so ISO 14289-1, 7.5 cannot determine the association
# algorithmically.
table_bad = elem("Table", document)
tr_bad = elem("TR", table_bad)
th_bad = elem("TH", tr_bad, kids=0)
td_bad = elem("TD", tr_bad, kids=1)
tr_bad.K = Array([th_bad, td_bad])
table_bad.K = Array([tr_bad])

# Table two conforms: the TH declares /Scope and an /ID, and the TD points at
# it through /Headers. A firing on this table would be a false positive, and
# its silence proves the check descends past the first table rather than
# latching on it.
table_ok = elem("Table", document)
tr_ok = elem("TR", table_ok)
th_ok = elem(
    "TH",
    tr_ok,
    kids=2,
    extra={"ID": String("h2"), "A": Dictionary(O=Name("/Table"), Scope=Name.Column)},
)
td_ok = elem(
    "TD",
    tr_ok,
    kids=3,
    extra={"A": Dictionary(O=Name("/Table"), Headers=Array([String("h2")]))},
)
tr_ok.K = Array([th_ok, td_ok])
table_ok.K = Array([tr_ok])

# A list whose second item omits the /Lbl its sibling carries. The nesting
# L > LI > LBody is what the ISO/TS 32005 Table 5 rows govern under PDF/UA-2.
lst = elem("L", document)
li_one = elem("LI", lst)
lbl_one = elem("Lbl", li_one)
lbody_one = elem("LBody", li_one, kids=4)
li_one.K = Array([lbl_one, lbody_one])
li_two = elem("LI", lst)
lbody_two = elem("LBody", li_two, kids=5)
li_two.K = Array([lbody_two])
lst.K = Array([li_one, li_two])

para = elem("P", document, kids=6)

document.K = Array([table_bad, table_ok, lst, para])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([th_bad, td_bad, th_ok, td_ok, lbody_one, lbody_two, para]),
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

attach_xmp(pdf, xmp_packet(ua_part="1"))
save(pdf, "tables_ua1.pdf", min_version="1.7")
