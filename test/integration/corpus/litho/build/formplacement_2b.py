#!/usr/bin/env python3
"""Build formplacement_2b.pdf: widgets whose appearance a viewer places by
ISO 32000-1, 12.5.5, Algorithm 8.1 somewhere a translation to /Rect does not,
at PDF/A-2b.

A viewer transforms an appearance's /BBox by its /Matrix, then scales and
translates the box it gets onto the annotation's /Rect. Flattening a form, and
the drawing of signed signatures every rewrite does, placed an appearance by
translating it to the first corner /Rect names, which is right only for a box
at the origin sized to /Rect. The corpora reach few such appearances, none
rotated by /Matrix, so this fixture gives the gate every case:

| Field     | /Rect                | Appearance /BBox  | /Matrix           | Viewer places it    |
| --------- | -------------------- | ----------------- | ----------------- | ------------------- |
| control   | [20 200 100 260]     | [0 0 80 60]       | none              | translated          |
| offset    | [120 200 200 260]    | [200 100 280 160] | none              | box moved to /Rect  |
| scaled    | [220 200 300 260]    | [0 0 40 30]       | none              | scaled by 2         |
| rotated   | [20 40 80 120]       | [0 0 80 60]       | 90 degrees        | turned upright      |
| combined  | [120 40 180 120]     | [10 20 50 50]     | 90 degrees        | turned, scaled by 2 |
| reversed  | [300 120 220 40]     | [0 0 80 80]       | translates 40,-30 | translation undone  |
| collapsed | [350 50 350 50]      | [0 0 20 20]       | none              | scaled to nothing   |

Each appearance fills its box in a light colour, a dark square in the box's
lower-left corner and a red border, so a misplaced, unturned or unscaled
drawing shows. reversed names its /Rect corners in the other order (ISO
32000-1, 7.9.5). collapsed has no area: a viewer draws nothing of it.

The fields are text fields that hold no value, so the appearance is all there
is to draw. Each widget is tagged under a Form element of its own, so the
flattened appearances are tagged content (formuntagged_ua1.pdf covers the
untagged case). The file
declares an RGB OutputIntent, and every widget prints.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
from _icc import rgb_display_profile

PRINT = 4

pdf = pikepdf.new()

destination = Stream(pdf, rgb_display_profile())
destination.N = 3
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho RGB display"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)


def num(v):
    return ("%g" % v).encode()


def appearance(bbox, fill, matrix=None):
    """A form that fills bbox, marks its lower-left corner and strokes its border."""
    x0, y0, x1, y1 = bbox
    w, h = x1 - x0, y1 - y0
    r, g, b = fill
    content = b" ".join(
        [
            num(r), num(g), num(b), b"rg",
            num(x0), num(y0), num(w), num(h), b"re f",
            b"0 0 0.5 rg",
            num(x0), num(y0), num(w / 3), num(h / 3), b"re f",
            b"0.7 0 0 RG 2 w",
            num(x0 + 1), num(y0 + 1), num(w - 2), num(h - 2), b"re S\n",
        ]
    )
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array(bbox)
    if matrix is not None:
        s.Matrix = Array(matrix)
    s.Resources = Dictionary()
    return pdf.make_indirect(s)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        MediaBox=Array([0, 0, 400, 300]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"")),
    )
)
pdf.Root.Pages.Kids = Array([page])
pdf.Root.Pages.Count = 1

QUARTER = [0, 1, -1, 0, 0, 0]
WIDGETS = [
    ("control", [20, 200, 100, 260], [0, 0, 80, 60], None, (0.85, 0.9, 1)),
    ("offset", [120, 200, 200, 260], [200, 100, 280, 160], None, (0.85, 1, 0.85)),
    ("scaled", [220, 200, 300, 260], [0, 0, 40, 30], None, (1, 0.95, 0.8)),
    ("rotated", [20, 40, 80, 120], [0, 0, 80, 60], QUARTER, (0.95, 0.85, 1)),
    ("combined", [120, 40, 180, 120], [10, 20, 50, 50], QUARTER, (0.85, 1, 1)),
    ("reversed", [300, 120, 220, 40], [0, 0, 80, 80], [1, 0, 0, 1, 40, -30], (1, 0.88, 0.88)),
    ("collapsed", [350, 50, 350, 50], [0, 0, 20, 20], None, (0.9, 0.9, 0.9)),
]

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))

fields, elements, nums = [], [], []
for key, (name, rect, bbox, matrix, fill) in enumerate(WIDGETS):
    widget = pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Widget,
            FT=Name.Tx,
            T=String(name),
            Rect=Array(rect),
            P=page,
            F=PRINT,
            StructParent=key,
            AP=Dictionary(N=appearance(bbox, fill, matrix)),
        )
    )
    element = pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Form,
            P=document,
            Pg=page,
            K=Dictionary(Type=Name.OBJR, Obj=widget),
        )
    )
    fields.append(widget)
    elements.append(element)
    nums.extend([key, element])

page.Annots = Array(fields)
pdf.Root.AcroForm = Dictionary(Fields=Array(fields))

# Each widget is tagged under a Form element of its own (ISO 32000-1,
# 14.8.4.4), so flattening draws each appearance as that element's content
# rather than as unmarked content, which PDF/UA forbids (ISO 14289-1, 7.1).
document.K = Array(elements)
struct_root.K = document
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array(nums)))
struct_root.ParentTreeNextKey = len(WIDGETS)
pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "formplacement_2b.pdf")
