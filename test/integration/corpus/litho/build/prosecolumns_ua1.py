#!/usr/bin/env python3
"""Build prosecolumns_ua1.pdf: two columns of prose set line by line on shared
baselines, and a borderless table as the control.

ISO 14289-1, 7.2 asks the structure tree to follow the reading order, and 7.5
reserves Table for content whose meaning is tabular. Auto-tagging builds the
tree from the page. Page 1 sets two columns of running text line by line
across both, each line a text object of its own and every right-hand line on
the baseline of its left-hand neighbour, with no rule anywhere. Every row of
the two aligned, so the table pass tagged the columns Table > TR > TD, a grid
of half-sentences read across the gutter. Each column now reads as one P, left
then right, as on readcolumns_ua1.pdf, whose baselines sit apart.

Page 2 is the control: a borderless table of three columns of short cells,
an item, a quantity and a price, still tagged Table > TR > TD.

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

LEFT = [
    b"Accessible documents carry a structure tree",
    b"that names what each part of a page is: a",
    b"heading, a paragraph, a list, a table or a",
    b"figure. Assistive technology reads that tree",
    b"rather than the order in which a producer",
    b"happened to draw the text, so a page set in",
    b"two columns reads one column after the other",
    b"when its tree follows the reading order.",
]

RIGHT = [
    b"Some producers set such a page line by line,",
    b"the first line of the left column and then",
    b"the first line of the right one, on the same",
    b"baseline. Rows of the two columns then line",
    b"up as the rows of a table do, yet nothing on",
    b"the page is tabular: no rule parts the rows,",
    b"and every line runs on into the next one of",
    b"its own column, never across the gutter.",
]

TABLE = [
    (b"Item", b"Quantity", b"Price"),
    (b"Paper", b"12", b"4.50"),
    (b"Toner", b"2", b"38.00"),
    (b"Staples", b"5", b"1.20"),
    (b"Folders", b"30", b"9.90"),
]

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def columns_page():
    """Page 1: a title over two prose columns sharing their baselines."""
    out = [b"BT /F1 16 Tf 72 720 Td (Two columns of prose on shared baselines) Tj ET\n"]
    for line, (left, right) in enumerate(zip(LEFT, RIGHT)):
        y = 680 - 12 * line
        out.append(b"BT /F1 10 Tf 72 %d Td (%s) Tj ET\n" % (y, left))
        out.append(b"BT /F1 10 Tf 320 %d Td (%s) Tj ET\n" % (y, right))
    return b"".join(out)


def table_page():
    """Page 2: a title over a borderless table of short cells."""
    out = [b"BT /F1 16 Tf 72 720 Td (A borderless table of short cells) Tj ET\n"]
    for row, cells in enumerate(TABLE):
        y = 680 - 16 * row
        for x, cell in zip((72, 220, 340), cells):
            out.append(b"BT /F1 10 Tf %d %d Td (%s) Tj ET\n" % (x, y, cell))
    return b"".join(out)


for content in (columns_page(), table_page()):
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

save(pdf, "prosecolumns_ua1.pdf", min_version="1.7")
