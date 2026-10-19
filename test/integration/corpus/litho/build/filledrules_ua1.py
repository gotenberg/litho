#!/usr/bin/env python3
"""Build filledrules_ua1.pdf: a table of wide cells ruled with filled hairline
rectangles, and two columns of prose under filled bars as the control.

ISO 14289-1, 7.5 reserves Table for content whose meaning is tabular, and 7.2
asks the structure tree to follow the reading order. Auto-tagging builds the
tree from the page, and reads a grid whose cells are lines of running text as
columns of prose unless a rule crosses its gutter. Page 1 is a table of two
columns of wide cells, each a single line of text, ruled the way Word draws a
table's borders: every border is a row of filled rectangles half a point
tall, one per cell and one square at each corner (`re f`), none of which
crosses the gutter alone, and the column borders are filled the same way. The
ruling is filled, not stroked, so the grid read as two columns of prose. ISO
32000-1, 8.5.3.3 paints a filled path as it does a stroked one, and the page
now reads as one Table > TR > TD.

Page 2 is the control: two columns of prose set line by line on shared
baselines, as on prosecolumns_ua1.pdf, under a page title a full-width filled
bar underlines, more than a line above the columns, with a light shaded panel
behind both columns that crosses the gutter but is as tall as the text. Each
column still reads as one P.

The file is untagged on purpose, so remediation to PDF/UA reaches the
auto-tagger. It declares PDF/UA-1, carries a title, a language and
DisplayDocTitle, so the rules that fire before remediation are the untagged
pages and the non-embedded font, and the font is the one residual remediation
leaves.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    String,
    Stream,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

TABLE = [
    (
        b"The structure tree follows the reading order",
        b"Clause 7.2, logical structure and order",
    ),
    (
        b"Tables are tagged with their rows and cells",
        b"Clause 7.5, table rows, headers and cells",
    ),
    (
        b"Every figure carries an alternative text",
        b"Clause 7.3, figures and their descriptions",
    ),
    (
        b"Running heads and folios are artifacts",
        b"Clause 7.8, page headers and page footers",
    ),
    (
        b"The document states its natural language",
        b"Clause 7.2, the Lang entry of the catalog",
    ),
]

LEFT = [
    b"Accessible documents carry a structure tree",
    b"that names what each part of a page is: a",
    b"heading, a paragraph, a list, a table or a",
    b"figure. Assistive technology reads that tree",
    b"rather than the order in which a producer",
    b"happened to draw the text, so a page set in",
]

RIGHT = [
    b"Some producers set such a page line by line,",
    b"the first line of the left column and then",
    b"the first line of the right one, on the same",
    b"baseline. A bar under the title and a panel",
    b"behind the text are no rules between rows,",
    b"so each column still reads as one paragraph.",
]

# Word's half-point border, and the cell edges of the table on page 1.
RULE = 0.48
EDGES = (66.0, 314.0, 546.0)

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def num(value):
    return b"%g" % value


def border(y):
    """A horizontal border filled cell by cell, a square at every corner."""
    out = []
    for i, x in enumerate(EDGES):
        out.append(b"%s %s %s %s re f*\n" % (num(x), num(y), num(RULE), num(RULE)))
        if i + 1 < len(EDGES):
            start = x + RULE
            out.append(
                b"%s %s %s %s re f*\n"
                % (num(start), num(y), num(EDGES[i + 1] - start), num(RULE))
            )
    return b"".join(out)


def table_page():
    """Page 1: a title over a table of wide cells ruled by filled hairlines."""
    out = [b"BT /F1 16 Tf 72 720 Td (A table ruled with filled hairlines) Tj ET\n"]
    top = 694.0
    pitch = 20.0
    out.append(border(top))
    for row, (left, right) in enumerate(TABLE):
        y = 680 - pitch * row
        out.append(b"BT /F1 10 Tf 72 %d Td (%s) Tj ET\n" % (y, left))
        out.append(b"BT /F1 10 Tf 320 %d Td (%s) Tj ET\n" % (y, right))
        out.append(border(top - pitch * (row + 1)))
    bottom = top - pitch * len(TABLE)
    for x in EDGES:
        out.append(
            b"%s %s %s %s re f*\n"
            % (num(x), num(bottom + RULE), num(RULE), num(top - bottom - RULE))
        )
    return b"".join(out)


def columns_page():
    """Page 2: a barred title over two prose columns on a shaded panel."""
    out = [
        b"q 0.92 g 66 600 480 92 re f Q\n",
        b"BT /F1 16 Tf 72 720 Td (Two columns of prose under a barred title) Tj ET\n",
        b"72 712 468 1 re f\n",
    ]
    for line, (left, right) in enumerate(zip(LEFT, RIGHT)):
        y = 680 - 12 * line
        out.append(b"BT /F1 10 Tf 72 %d Td (%s) Tj ET\n" % (y, left))
        out.append(b"BT /F1 10 Tf 320 %d Td (%s) Tj ET\n" % (y, right))
    return b"".join(out)


for content in (table_page(), columns_page()):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(Font=Dictionary(F1=font)),
            Contents=Stream(pdf, content),
        )
    )
    pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(ua_part=1))
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

save(pdf, "filledrules_ua1.pdf", min_version="1.7")
