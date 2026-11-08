#!/usr/bin/env python3
"""Build boxorigin_2b.pdf: pages whose visible lower-left corner is not the
user-space origin, at PDF/A-2b.

A page's content draws in user space, where (0, 0) is the coordinate origin,
but a viewer shows the crop box (ISO 32000-1, 14.11.2), and nothing makes the
crop box start at the origin. A caller who places a stamp "from the page's
bottom-left corner" names that visible corner, so litho translates the stamp
by the box's lower-left point. A rectangle may name either pair of opposite
corners (ISO 32000-1, 7.9.5), so that point is the lesser of each pair, and a
crop box reaching past the media box is reduced to their intersection
(14.11.2.1) before its corner counts.

| Page | Boundaries as written                            | Visible lower-left |
| ---- | ------------------------------------------------ | ------------------ |
| 1    | /MediaBox [0 0 612 792] /CropBox [50 60 500 700] | (50, 60)           |
| 2    | /MediaBox [-306 -396 306 396]                    | (-306, -396)       |
| 3    | /MediaBox [306 396 -306 -396]                    | (-100, -150)       |
|      | /CropBox [200 300 -100 -150]                     |                    |
| 4    | /MediaBox [0 0 612 792] /CropBox [30 40 700 900] | (30, 40)           |
| 5    | /CropBox [100 120 500 700] /Rotate 90            | (100, 120)         |
| 6    | /CropBox [70 80 400 500] from its parent node    | (70, 80)           |

Pages 5 and 6 state /MediaBox [0 0 612 792]. No page before this fixture had
a crop box that did not start at the origin, so the mutate-stamp gate cell
could not tell a stamp placed at the corner from one placed at the origin or
moved by the box origin twice. No page draws anything, so every page reads the
same under every mutation, and the file conforms at PDF/A-2b.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

root = pdf.make_indirect(Dictionary(Type=Name.Pages, Resources=Dictionary()))


def page(parent, **boxes):
    return pdf.make_indirect(Dictionary(Type=Name.Page, Parent=parent, **boxes))


letter = [0, 0, 612, 792]
page1 = page(root, MediaBox=Array(letter), CropBox=Array([50, 60, 500, 700]))
page2 = page(root, MediaBox=Array([-306, -396, 306, 396]))
page3 = page(
    root,
    MediaBox=Array([306, 396, -306, -396]),
    CropBox=Array([200, 300, -100, -150]),
)
page4 = page(root, MediaBox=Array(letter), CropBox=Array([30, 40, 700, 900]))
page5 = page(
    root, MediaBox=Array(letter), CropBox=Array([100, 120, 500, 700]), Rotate=90
)
node = pdf.make_indirect(
    Dictionary(Type=Name.Pages, Parent=root, CropBox=Array([70, 80, 400, 500]))
)
page6 = page(node, MediaBox=Array(letter))

node.Kids = Array([page6])
node.Count = 1
root.Kids = Array([page1, page2, page3, page4, page5, node])
root.Count = 6

# The tree is built by hand: pikepdf's page list flattens it, which would
# move the inherited crop box onto the page.
pdf.Root.Pages = root

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "boxorigin_2b.pdf", min_version="1.7")
