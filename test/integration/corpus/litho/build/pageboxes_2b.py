#!/usr/bin/env python3
"""Build pageboxes_2b.pdf: page boundaries that are not rectangles, or enclose
no area, beside the crop box of the root they sit under, which every rewrite
writes as a viewer reads them, at PDF/A-2b.

ISO 32000-1, 7.7.3.3, Table 30 makes each page boundary a rectangle, four
numbers (7.9.5), and gives each but the media box a default: the media box for
the crop box, the crop box for the bleed, trim and art boxes. veraPDF measures
a box on its first four entries, a missing one or one that is no number as 0.
pdf.js and poppler read a box that is not four numbers enclosing an area as no
box, and draw the page on its default, US Letter for the media box. A rewrite
copied a crop, bleed, trim or art box as its source stated it, and wrote US
Letter for a media box that is not four numbers. The corpus holds a media box
of more than four entries only where its first four are Letter
(directstream_2b.pdf), and no such box on a page tree node, nor one beside a
crop box a node above states.

The root states /MediaBox [0 0 612 792] and /CropBox [0 0 500 700]. A node
under it states /MediaBox [0 0 400 500 /X] and /CropBox [0 0 0 0], and lists
pages 2 and 3:

| Page | Box        | As written       | veraPDF measures | A viewer reads | Written as    |
| ---- | ---------- | ---------------- | ---------------- | -------------- | ------------- |
| 1    | /MediaBox  | [0 0 600 800 7]  | [0 0 600 800]    | US Letter      | US Letter     |
| 1    | /CropBox   | [0 0 300]        | no area          | the media box  | the media box |
| 1    | /TrimBox   | [0 /Foo 200 300] | [0 0 200 300]    | the crop box   | none          |
| 1    | /BleedBox  | [0 0 300 400 9]  | [0 0 300 400]    | the crop box   | none          |
| 2    | /MediaBox  | the node's       | [0 0 400 500]    | US Letter      | US Letter     |
| 2    | /CropBox   | the node's       | no area          | the media box  | the media box |
| 3    | /MediaBox  | [0 0 612]        | no area          | US Letter      | US Letter     |
| 3    | /CropBox   | the node's       | no area          | the media box  | the media box |

A crop, bleed, trim or art box a viewer does not use reads as its default, and
the output drops it. Dropping page 1's crop box, or the node's, would let the
root's show through, so each page states its media box as its crop box instead
and shows the region it showed. A media box a viewer does not use reads as US
Letter, and the output writes Letter.

The crop box of page 1, the one pages 2 and 3 inherit and the media box of
page 3 fail ISO 19005-2, 6.1.13 t11 as veraPDF measures them, four checks.
Every other box passes it, and every output passes it.

No text is drawn, so the pages read the same under every mutation.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

root = pdf.make_indirect(
    Dictionary(
        Type=Name.Pages,
        MediaBox=Array([0, 0, 612, 792]),
        CropBox=Array([0, 0, 500, 700]),
        Resources=Dictionary(),
    )
)
node = pdf.make_indirect(
    Dictionary(
        Type=Name.Pages,
        Parent=root,
        MediaBox=Array([0, 0, 400, 500, Name("/X")]),
        CropBox=Array([0, 0, 0, 0]),
    )
)
page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=root,
        MediaBox=Array([0, 0, 600, 800, 7]),
        CropBox=Array([0, 0, 300]),
        TrimBox=Array([0, Name("/Foo"), 200, 300]),
        BleedBox=Array([0, 0, 300, 400, 9]),
    )
)
page2 = pdf.make_indirect(Dictionary(Type=Name.Page, Parent=node))
page3 = pdf.make_indirect(Dictionary(Type=Name.Page, Parent=node, MediaBox=Array([0, 0, 612])))

node.Kids = Array([page2, page3])
node.Count = 2
root.Kids = Array([page1, node])
root.Count = 3

# The tree is built by hand: pikepdf's page list flattens it, which would
# delete the node the fixture exists for.
pdf.Root.Pages = root

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "pageboxes_2b.pdf", min_version="1.7")
