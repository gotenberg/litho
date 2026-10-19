#!/usr/bin/env python3
"""Build readorder_ua1.pdf: an untagged two-column page set out of order.

ISO 14289-1, 7.2 asks the structure tree to follow the reading order, and
auto-tagging builds that tree from the page. The content stream here does not
follow it. A title spans both columns, and the stream draws it last. The
paragraphs of the two columns alternate, left, right, left, right, and the
right column carries a smaller heading of its own, drawn before the title. A
page number at the foot is drawn first.

A reader takes the title, the left column, then the right one. Read in content
order, the tree put the right column's heading first, tagged it H1 with the
title under it at the same level, and folded the right column's first
paragraph and the left column's second, adjacent in the stream and a line apart
in text space, into one paragraph that crossed the gutter.

The file is untagged on purpose, so remediation to PDF/UA reaches the
auto-tagger. It declares PDF/UA-1, carries a title, a language and
DisplayDocTitle, so the rules that fire before remediation are the untagged
page and the non-embedded font, and the font is the one residual remediation
leaves.

See ../README.md for the authoritative expected firing set.
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

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def para(label, x, y, lines):
    """One text object setting lines down from (x, y) at a 12-point leading."""
    shown = b" T* ".join(
        b"(%s, line %d.) Tj" % (label, i + 1)
        for i in range(lines)
    )
    return b"BT /F1 10 Tf 12 TL %d %d Td %s ET\n" % (x, y, shown)


content = (
    b"BT /F1 9 Tf 300 40 Td (1) Tj ET\n"
    + b"BT /F1 14 Tf 320 712 Td (Right heading) Tj ET\n"
    + para(b"Left, paragraph one", 72, 696, 3)
    + para(b"Right, paragraph one", 320, 696, 5)
    + para(b"Left, paragraph two", 72, 636, 4)
    + para(b"Right, paragraph two", 320, 612, 3)
    + b"BT /F1 24 Tf 72 740 Td (A title across both columns of the page) Tj ET\n"
)

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

save(pdf, "readorder_ua1.pdf", min_version="1.7")
