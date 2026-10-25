#!/usr/bin/env python3
"""Build noteicons_2b.pdf: Text annotations with no appearance stream, one
for each predefined icon name, at PDF/A-2b.

A closed Text annotation appears as an icon (ISO 32000-1, 12.5.6.4), and Table
172 names the icons a viewer provides: Comment, Key, Note (the default), Help,
NewParagraph, Paragraph and Insert. The renderer draws its own icon for each,
24 units square from the upper-left corner of /Rect, its background /C. Text
annotations neither scale nor rotate with the page (12.5.6.4). The render
goldens of the corpora held only Text annotations with no /Name and no /C, on
unturned pages, so the render cell compared no named icon, no background and
no upright icon on a turned page.

Page 1, 200 by 200, holds the seven names, an unknown name (/Star, drawn as
Note) and a Note with no /C and no /Name, all outside the centred quarter and
the band across the middle, except a Note whose /Rect is 4 units square at
(60, 136): its icon hangs to (84, 116), inside the quarter, which removes it.
The Key's background is dark, so its symbol is white.

Page 2, 200 by 200, turns 90 degrees clockwise. A Comment whose /Rect is 4
units square at (60, 66) hangs its icon upright from (60, 70): in default user
space it spans x 60 to 84 and y 70 to 94, inside the centred quarter, while
the icon turned with the page would reach y 46, across its edge.

Each annotation sets /F 4 and lacks the /AP that ISO 19005-2, 6.3.3 requires,
so 6.3.3 t1 fires. The file declares no OutputIntent, and the /C arrays are
the only colour it uses.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def note(rect, name=None, colour=None):
    d = Dictionary(Type=Name.Annot, Subtype=Name.Text, Rect=Array(rect), F=4)
    if name:
        d.Name = Name("/" + name)
    if colour:
        d.C = Array(colour)
    return pdf.make_indirect(d)


page1 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 200]),
    Resources=Dictionary(),
    Annots=Array(
        [
            note([10, 166, 34, 190], "Comment", [1, 1, 0]),
            note([45, 166, 69, 190], "Key", [0, 0, 0.6]),
            note([80, 166, 104, 190], "Note", [0.7, 0.85, 1]),
            note([115, 166, 139, 190], "Help", [0.6, 0.9, 0.6]),
            note([150, 166, 174, 190], "NewParagraph", [1, 0.8, 0.6]),
            note([10, 10, 34, 34], "Paragraph", [0.9, 0.9, 0.9]),
            note([45, 10, 69, 34], "Insert", [1, 0.6, 0.6]),
            note([80, 10, 104, 34], "Star", [1, 1, 0.6]),
            note([150, 10, 174, 34]),
            note([60, 136, 64, 140], None, [0.8, 0.8, 1]),
        ]
    ),
)
page2 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 200]),
    Rotate=90,
    Resources=Dictionary(),
    Annots=Array([note([60, 66, 64, 70], "Comment", [1, 0.6, 0.6])]),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page1)))
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page2)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "noteicons_2b.pdf", min_version="1.7")
