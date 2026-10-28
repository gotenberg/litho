#!/usr/bin/env python3
"""Build structnest_ua1.pdf: a PDF/UA-1 fixture for ISO 14289-1, 7.2.

Despite its title, 7.2 is a mixed clause. It governs three unrelated things:
where a table, list and table-of-contents element may sit in the structure
tree, where a Caption may sit among its siblings, and whose natural language
covers a piece of text that is not page content. No other fixture reaches any
of it beyond the two document-wide language tests.

Every defect is a separate element, and nine of them are siblings under one
Div, so a check that reports the first misplaced element and stops reports one
rule where nine are due. Six controls conform in the same respects the defects
break.

The page carries no text, only artifact and tagged vector content, so the
document-wide language tests stay out of the way of the per-element ones.

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
    xmp_packet,
)

pdf = pikepdf.new()

# Four /Span marked-content sequences, three carrying text in a property whose
# language nothing determines. The fourth declares /Lang inline and is the
# control. Each wraps a stroked path rather than a glyph run, so the sequences
# are real tagged content without pulling a font into the fixture.
content = b"""/Artifact BMC
q 0.6 w 40 752 m 560 752 l S Q
EMC
/Span <</MCID 0 /ActualText (Marked content actual text)>> BDC
q 40 720 m 200 720 l S Q
EMC
/Span <</MCID 1 /Alt (Marked content alternate description)>> BDC
q 40 700 m 200 700 l S Q
EMC
/Span <</MCID 2 /E (MC)>> BDC
q 40 680 m 200 680 l S Q
EMC
/Span <</MCID 3 /Lang (en-US) /ActualText (Language declared inline)>> BDC
q 40 660 m 200 660 l S Q
EMC
"""

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
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)


def elem(stype, parent, kids=None, **extra):
    """Create one structure element, wired to parent and to the page."""
    d = Dictionary(Type=Name.StructElem, S=Name("/" + stype), P=parent, Pg=page)
    if kids is not None:
        d.K = kids
    for key, value in extra.items():
        setattr(d, key, value)
    return pdf.make_indirect(d)


def scoped_th(parent):
    """Create a TH carrying /Scope, so 7.5 stays out of the 7.2 rule set."""
    return elem("TH", parent, A=Dictionary(O=Name("/Table"), Scope=Name.Column))


def row(parent, cells=1, cell=scoped_th):
    """Create a TR holding the given number of cells."""
    tr = elem("TR", parent)
    tr.K = Array([cell(tr) for _ in range(cells)])
    return tr


def section(stype, parent):
    """Create a THead/TBody/TFoot holding one TR."""
    sec = elem(stype, parent)
    sec.K = Array([row(sec)])
    return sec


# Nine misplaced siblings under one Div. Each breaks a different containment
# test, and none of them is inside a Table, an L or a TOC, so no enclosing
# element can absorb the report.
orphans = elem("Div", document)
orphan_tr = row(orphans)  # t4: TR outside Table/THead/TBody/TFoot
orphan_thead = section("THead", orphans)  # t5: THead outside Table
orphan_tbody = section("TBody", orphans)  # t6: TBody outside Table
orphan_tfoot = section("TFoot", orphans)  # t7: TFoot outside Table
orphan_th = scoped_th(orphans)  # t8: TH outside TR
orphan_td = elem("TD", orphans)  # t9: TD outside TR
orphan_li = elem("LI", orphans)  # t17: LI outside L
orphan_li.K = Array([elem("LBody", orphan_li)])
orphan_lbody = elem("LBody", orphans)  # t18: LBody outside LI
orphan_toci = elem("TOCI", orphans)  # t26: TOCI outside TOC
orphans.K = Array(
    [
        orphan_tr,
        orphan_thead,
        orphan_tbody,
        orphan_tfoot,
        orphan_th,
        orphan_td,
        orphan_li,
        orphan_lbody,
        orphan_toci,
    ]
)

# t3: a Table kid that is none of TR, THead, TBody, TFoot, Caption.
tbl_kid = elem("Table", document)
tbl_kid.K = Array([row(tbl_kid), elem("P", tbl_kid)])

# t10: a TR kid that is neither TH nor TD.
tbl_trkid = elem("Table", document)
tr_trkid = elem("TR", tbl_trkid)
tr_trkid.K = Array([scoped_th(tr_trkid), elem("P", tr_trkid)])
tbl_trkid.K = Array([tr_trkid])

# t11: two THead kids. The TBody keeps t14 out of this table.
tbl_2thead = elem("Table", document)
tbl_2thead.K = Array(
    [
        section("THead", tbl_2thead),
        section("THead", tbl_2thead),
        section("TBody", tbl_2thead),
    ]
)

# t12: two TFoot kids. The TBody keeps t13 out of this table.
tbl_2tfoot = elem("Table", document)
tbl_2tfoot.K = Array(
    [
        section("TBody", tbl_2tfoot),
        section("TFoot", tbl_2tfoot),
        section("TFoot", tbl_2tfoot),
    ]
)

# t13: a TFoot with no TBody beside it.
tbl_tfoot = elem("Table", document)
tbl_tfoot.K = Array([section("TFoot", tbl_tfoot)])

# t14: a THead with no TBody beside it.
tbl_thead = elem("Table", document)
tbl_thead.K = Array([section("THead", tbl_thead)])

# t16: a Caption that is neither the first nor the last Table kid.
tbl_capmid = elem("Table", document)
tbl_capmid.K = Array(
    [row(tbl_capmid), elem("Caption", tbl_capmid), row(tbl_capmid)]
)

# t39: two Captions. Both sit in a position t16 allows, so only the count
# fails.
tbl_2cap = elem("Table", document)
tbl_2cap.K = Array(
    [elem("Caption", tbl_2cap), row(tbl_2cap), elem("Caption", tbl_2cap)]
)

# t36, t37, t38: a section holding something other than a TR. Each is a
# separate table so the three tests cannot mask one another.
tbl_hkid = elem("Table", document)
thead_hkid = elem("THead", tbl_hkid)
thead_hkid.K = Array([elem("P", thead_hkid)])
tbl_hkid.K = Array([thead_hkid, section("TBody", tbl_hkid)])

tbl_bkid = elem("Table", document)
tbody_bkid = elem("TBody", tbl_bkid)
tbody_bkid.K = Array([elem("P", tbody_bkid)])
tbl_bkid.K = Array([tbody_bkid])

tbl_fkid = elem("Table", document)
tfoot_fkid = elem("TFoot", tbl_fkid)
tfoot_fkid.K = Array([elem("P", tfoot_fkid)])
tbl_fkid.K = Array([section("TBody", tbl_fkid), tfoot_fkid])

# t42 and t43 split the same irregularity by direction. veraPDF sizes the grid
# from the first row and the row count, then walks the cells: a cell that
# overflows the last column is t42, and a row that ends before the last column
# is t43.
tbl_wide = elem("Table", document)
tbl_wide.K = Array([row(tbl_wide), row(tbl_wide, cells=2)])

tbl_short = elem("Table", document)
tbl_short.K = Array([row(tbl_short, cells=2), row(tbl_short)])

# Control table. Caption first, one THead, one TBody, one TFoot, every TR
# holding one cell, the TH scoped over its column so both data cells resolve a
# header. It conforms in every respect the thirteen tables above break.
tbl_ok = elem("Table", document)
thead_ok = section("THead", tbl_ok)
tbody_ok = elem("TBody", tbl_ok)
tbody_ok.K = Array([row(tbody_ok, cell=lambda p: elem("TD", p))])
tfoot_ok = elem("TFoot", tbl_ok)
tfoot_ok.K = Array([row(tfoot_ok, cell=lambda p: elem("TD", p))])
tbl_ok.K = Array([elem("Caption", tbl_ok), thead_ok, tbody_ok, tfoot_ok])


def list_item(parent, labelled=False):
    """Create an LI holding an LBody, optionally preceded by an Lbl."""
    li = elem("LI", parent)
    kids = [elem("Lbl", li)] if labelled else []
    kids.append(elem("LBody", li))
    li.K = Array(kids)
    return li


# t19: an L kid that is none of L, LI, Caption.
lst_kid = elem("L", document)
lst_kid.K = Array([list_item(lst_kid), elem("P", lst_kid)])

# t20: an LI kid that is neither Lbl nor LBody.
lst_likid = elem("L", document)
li_likid = elem("LI", lst_likid)
li_likid.K = Array([elem("P", li_likid)])
lst_likid.K = Array([li_likid])

# t40: a Caption that is not the first L kid. t16 allows a last-position
# Caption in a Table and t40 does not allow one in an L, which is the whole
# difference between the two tests.
lst_cap = elem("L", document)
lst_cap.K = Array([list_item(lst_cap), elem("Caption", lst_cap)])

# Control list: L > LI > Lbl + LBody, the nesting the three lists above break.
lst_ok = elem("L", document)
lst_ok.K = Array([list_item(lst_ok, labelled=True)])

# t27: a TOC kid that is none of TOC, TOCI, Caption.
toc_kid = elem("TOC", document)
toc_kid.K = Array([elem("TOCI", toc_kid), elem("P", toc_kid)])

# t28: a Caption that is not the first TOC kid.
toc_cap = elem("TOC", document)
toc_cap.K = Array([elem("TOCI", toc_cap), elem("Caption", toc_cap)])

# Control table of contents: Caption first, then a TOCI.
toc_ok = elem("TOC", document)
toc_ok.K = Array([elem("Caption", toc_ok), elem("TOCI", toc_ok)])

# t21, t22, t23: text carried by a structure attribute rather than by page
# content. Nothing in the /P chain and nothing in the catalog declares a
# language, so none of the three is determinable.
attr_actualtext = elem("Span", document, ActualText=String("Structure actual text"))
attr_alt = elem("Span", document, Alt=String("Structure alternate description"))
attr_expansion = elem("Span", document, E=String("SE"))

# Control: the same construct carrying its own /Lang. Its silence is what
# proves t21 reports per element rather than once per document.
attr_ok = elem(
    "Span", document, ActualText=String("Covered actual text"), Lang=String("en-US")
)

# The four /Span marked-content sequences hang off structure elements that
# declare no language of their own, so only the inline /Lang on the fourth
# sequence can cover it.
mc_parent = elem("P", document)
mc_actualtext = elem("Span", mc_parent, kids=0)
mc_alt = elem("Span", mc_parent, kids=1)
mc_expansion = elem("Span", mc_parent, kids=2)
mc_ok = elem("Span", mc_parent, kids=3)
mc_parent.K = Array([mc_actualtext, mc_alt, mc_expansion, mc_ok])

document.K = Array(
    [
        orphans,
        tbl_kid,
        tbl_trkid,
        tbl_2thead,
        tbl_2tfoot,
        tbl_tfoot,
        tbl_thead,
        tbl_capmid,
        tbl_2cap,
        tbl_hkid,
        tbl_bkid,
        tbl_fkid,
        tbl_wide,
        tbl_short,
        tbl_ok,
        lst_kid,
        lst_likid,
        lst_cap,
        lst_ok,
        toc_kid,
        toc_cap,
        toc_ok,
        attr_actualtext,
        attr_alt,
        attr_expansion,
        attr_ok,
        mc_parent,
    ]
)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([mc_actualtext, mc_alt, mc_expansion, mc_ok])]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# t2: an outline entry draws its natural language from the catalog alone, and
# this catalog declares none.
outline_item = pdf.make_indirect(Dictionary(Title=String("Nested structures")))
outlines = pdf.make_indirect(
    Dictionary(Type=Name.Outlines, First=outline_item, Last=outline_item, Count=1)
)
outline_item.Parent = outlines
pdf.Root.Outlines = outlines

# t25: a form field whose /TU tooltip has no determinable language, beside one
# that declares /Lang itself.
field_tu = pdf.make_indirect(
    Dictionary(FT=Name.Tx, T=String("undetermined"), TU=String("Tooltip text"))
)
field_ok = pdf.make_indirect(
    Dictionary(
        FT=Name.Tx,
        T=String("determined"),
        TU=String("Tooltip text"),
        Lang=String("en-US"),
    )
)
pdf.Root.AcroForm = Dictionary(Fields=Array([field_tu, field_ok]))

attach_xmp(pdf, xmp_packet(ua_part="1"))
save(pdf, "structnest_ua1.pdf")
