#!/usr/bin/env python3
"""Build tableregular_ua2.pdf: table regularity under PDF/UA-2.

The coverage scan showed ISO 14289-2, 8.2.5.26 as the largest unreached cluster
in the corpus's weakest profile. `tablespan_ua2.pdf` reaches test 3 through a
row whose column spans do not add up, and its docstring records tests 1 and 2 as
constructs it could not produce. Both are produced here.

Test 1 is scoped to `SETableCell` and test 2 to `SETable`, so the two are
different objects of one clause. Planting them together is the masking probe: a
check that latches on the first table it finds irregular reports one rule where
two are due. Two conforming tables sit beside them, one flat and one sectioned,
so a check that condemns every table once it sees one is equally visible.

The intersection in table A is not the naive one. A `/RowSpan` of 2 does not
collide with the row below it, because the table model shifts that row's cells
past the occupied slot rather than overlapping them. An intersection needs a
row whose own spans reach into a slot a previous row already carries down into:
here row 1 is TH(RowSpan 2), TH, TH(RowSpan 2) and row 2 is a single
TH(ColSpan 2), whose second column lands on the slot the last cell of row 1
occupies.

Table B separates the two tests that a section boundary decides. Its `/RowSpan`
starts in the THead and reaches into the TBody. Measured on both tools, that is
test 2 under PDF/UA-2 and NOT a column-count defect. The same table under
PDF/UA-1 reports the column rule 7.2 t42 instead, because PDF/UA-1 draws no
section boundary, so the two profiles disagree by design and the fixture
exercises both.

Tables E and F reach tests 5 and 6, the two halves of header determination.
Getting there needed the order veraPDF applies, which no amount of probing
revealed and which the clause does not state: determination is settled by
`/Scope` first, and the `/Headers` route runs only when at least one TH carries
no `/Scope`. Both tables therefore leave their single TH unscoped. The cell that
cannot be resolved sits at row 1 column 1, where no TH shares its row or its
column, and the cell at row 0 column 0 is exempt from the fallback altogether,
which is why the TH occupies it. Table E's cell names no header at all (t5) and table F's names one no TH
declares (t6). veraPDF stops at the first cell it cannot resolve, so the two
halves need a table each, and table F's earlier cells all carry a valid
/Headers so its target cell is the one that fails.

Tests 3 and 4 belong to `tablespan_ua2.pdf` and are not repeated here.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

CELL_COUNT = 32
content = b"".join(
    b"/TD <</MCID %d>> BDC\nBT /F1 9 Tf 40 %d Td (c%d) Tj ET\nEMC\n"
    % (i, 750 - i * 17, i)
    for i in range(CELL_COUNT)
)

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

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(
    Dictionary(Type=Name.StructTreeRoot, Namespaces=Array([ns20]))
)
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ns20,
        Lang=String("en-US"),
    )
)

leaves = []
next_mcid = 0


def cell(kind, parent, colspan=None, rowspan=None, scope="Column", cid=None, headers=None):
    """One TH or TD, owning the next marked content sequence on the page.

    scope=None omits /Scope from a TH, which is what pushes header
    determination onto the /Headers route.
    """
    global next_mcid
    attrs = {"O": Name("/Table")}
    if colspan is not None:
        attrs["ColSpan"] = colspan
    if rowspan is not None:
        attrs["RowSpan"] = rowspan
    if kind == "TH" and scope is not None:
        attrs["Scope"] = Name("/" + scope)
    if headers is not None:
        attrs["Headers"] = Array([String(h) for h in headers])
    d = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + kind),
        P=parent,
        NS=ns20,
        Pg=page,
        K=next_mcid,
        A=Dictionary(**attrs),
    )
    if cid is not None:
        d.ID = String(cid)
    next_mcid += 1
    made = pdf.make_indirect(d)
    leaves.append(made)
    return made


def row(parent, build):
    r = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.TR, P=parent, NS=ns20, Pg=page)
    )
    r.K = Array(build(r))
    return r


def section(kind, parent, builds):
    s = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name("/" + kind), P=parent, NS=ns20, Pg=page)
    )
    s.K = Array([row(s, b) for b in builds])
    return s


def table(kids):
    t = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.Table, P=document, NS=ns20, Pg=page)
    )
    t.K = Array(kids(t))
    return t


# Table A: row 2's second column lands on the slot row 1's last cell carries
# down into, which is a genuine cell intersection (t1).
table_a = table(
    lambda t: [
        row(
            t,
            lambda r: [
                cell("TH", r, rowspan=2, cid="a1"),
                cell("TH", r, cid="a2"),
                cell("TH", r, rowspan=2, cid="a3"),
            ],
        ),
        row(t, lambda r: [cell("TH", r, colspan=2, cid="a4")]),
    ]
)

# Table B: the /RowSpan starts in the THead and reaches into the TBody, so the
# grouping the sections form is not regular (t2).
table_b = table(
    lambda t: [
        section(
            "THead",
            t,
            [lambda r: [cell("TH", r, rowspan=2, cid="b1"), cell("TH", r, cid="b2")]],
        ),
        section("TBody", t, [lambda r: [cell("TD", r), cell("TD", r)]]),
    ]
)

# Table C conforms and is sectioned, so it shows the section model accepts a
# regular grouping rather than merely rejecting every THead.
table_c = table(
    lambda t: [
        section(
            "THead",
            t,
            [lambda r: [cell("TH", r, cid="c1"), cell("TH", r, cid="c2")]],
        ),
        section(
            "TBody",
            t,
            [
                lambda r: [cell("TD", r), cell("TD", r)],
                lambda r: [cell("TD", r), cell("TD", r)],
            ],
        ),
    ]
)

# Table D conforms while carrying real spans, so its silence shows the geometry
# check accepts a legitimate span rather than ignoring the attributes.
table_d = table(
    lambda t: [
        row(t, lambda r: [cell("TH", r, cid="d1"), cell("TH", r, cid="d2")]),
        row(t, lambda r: [cell("TD", r, colspan=2)]),
    ]
)

# Table E: regular, and its only TH carries no /Scope, so header determination
# falls through to the /Headers route. The cell at row 1 column 1 has no TH in
# its row or its column, so no header can be determined for it (t5). The cell
# at row 0 column 0 is exempt, which is why the TH sits there.
table_e = table(
    lambda t: [
        row(t, lambda r: [cell("TH", r, scope=None, cid="e1"), cell("TD", r), cell("TD", r)]),
        row(t, lambda r: [cell("TD", r), cell("TD", r), cell("TD", r)]),
    ]
)

# Table F is the other half of the same split: the undeterminable cell names a
# header id no TH declares (t6). Every cell before it carries a valid /Headers
# so that it is the FIRST cell veraPDF cannot resolve, which matters because
# the fallback stops there. It also makes the fixture profile-agnostic: under
# PDF/UA-1 an unscoped TH has no default scope at all, so without the valid
# /Headers an earlier cell would fail first and the file would report the other
# half of the split under the two profiles.
table_f = table(
    lambda t: [
        row(
            t,
            lambda r: [
                cell("TH", r, scope=None, cid="f1"),
                cell("TD", r, headers=["f1"]),
                cell("TD", r, headers=["f1"]),
            ],
        ),
        row(
            t,
            lambda r: [
                cell("TD", r, headers=["f1"]),
                cell("TD", r, headers=["nosuchheader"]),
                cell("TD", r, headers=["f1"]),
            ],
        ),
    ]
)

document.K = Array([table_a, table_b, table_c, table_d, table_e, table_f])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array(leaves)])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /Metadata, and the font is not embedded, so a table verdict
# is visibly separate from the metadata and font clauses.
save(pdf, "tableregular_ua2.pdf", min_version="2.0")
