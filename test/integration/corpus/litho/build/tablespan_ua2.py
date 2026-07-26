#!/usr/bin/env python3
"""Build tablespan_ua2.pdf: table cell geometry under PDF/UA-2.

The coverage scan showed ISO 14289-2, 8.2.5.26 unreached. It constrains the
geometry a table's cells describe through /RowSpan and /ColSpan: cells must
not overlap, and the spans a row or column declares must add up. The existing
`tables_ua1.pdf` covers header association under PDF/UA-1 but declares no
spans at all, so none of this clause is reachable from it.

Two tables: one whose row declares spans that do not add up, and one
conforming. The conforming table carries real spans rather than a trivial
grid, so its silence shows the geometry check accepts a legitimate span
rather than merely ignoring the attribute.

Only `t3` of the clause is reached. The overlap test `t1` needs two cells
claiming one grid position, which a `/RowSpan` cannot produce: under the
table model a spanning cell shifts the following row's cells rather than
colliding with them, so the construct reads as a column count mismatch, and
veraPDF agrees it is not an intersection. `t2` and `t4` need span geometries
this builder could not produce, and `t5`/`t6` are the header association
`tables_ua1.pdf` already covers.

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

# One marked content sequence per cell across both tables.
parts = []
for i in range(8):
    parts.append(
        b"/TD <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (c%d) Tj ET\nEMC\n"
        % (i, 700 - i * 20, i)
    )
content = b"".join(parts)

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

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        Lang=String("en-US"),
    )
)


def cell(kind, parent, mcid, colspan=None, rowspan=None, headers=None, cid=None):
    attrs = {"O": Name("/Table")}
    if colspan is not None:
        attrs["ColSpan"] = colspan
    if rowspan is not None:
        attrs["RowSpan"] = rowspan
    if headers is not None:
        attrs["Headers"] = Array([String(h) for h in headers])
    if kind == "TH":
        attrs["Scope"] = Name.Column
    d = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + kind),
        P=parent,
        Pg=page,
        K=mcid,
        A=Dictionary(**attrs),
    )
    if cid is not None:
        d.ID = String(cid)
    return pdf.make_indirect(d)


def row(parent, cells):
    r = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.TR, P=parent, Pg=page)
    )
    r.K = Array(cells(r))
    return r


# Table one declares a two column grid, then gives its second row a cell
# spanning three columns. The row's spans no longer add up to the table's
# width, which is what 8.2.5.26 constrains.
t_bad = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Table, P=document, Pg=page)
)
bad_r1 = row(t_bad, lambda r: [cell("TH", r, 0, cid="h1"), cell("TH", r, 1, cid="h2")])
bad_r2 = row(
    t_bad,
    lambda r: [
        cell("TD", r, 2, colspan=3, headers=["h1"]),
        cell("TD", r, 3, headers=["h2"]),
    ],
)
t_bad.K = Array([bad_r1, bad_r2])

# Table two is a genuine spanning layout: two header columns, then one row
# whose first cell spans both. The spans add up, every cell points at a
# header, so nothing here may fire.
t_ok = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Table, P=document, Pg=page)
)
ok_r1 = row(t_ok, lambda r: [cell("TH", r, 4, cid="g1"), cell("TH", r, 5, cid="g2")])
ok_r2 = row(t_ok, lambda r: [cell("TD", r, 6, colspan=2, headers=["g1", "g2"])])
ok_r3 = row(
    t_ok, lambda r: [cell("TD", r, 7, headers=["g1"]), cell("TD", r, 3, headers=["g2"])]
)
t_ok.K = Array([ok_r1, ok_r2, ok_r3])

document.K = Array([t_bad, t_ok])
struct_root.K = Array([document])

# Every cell owns exactly one marked content sequence, wired through the
# parent tree so veraPDF resolves each cell to its content.
order = [
    bad_r1.K[0],
    bad_r1.K[1],
    bad_r2.K[0],
    bad_r2.K[1],
    ok_r1.K[0],
    ok_r1.K[1],
    ok_r2.K[0],
    ok_r3.K[0],
]
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array(order)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

save(pdf, "tablespan_ua2.pdf", min_version="2.0")
