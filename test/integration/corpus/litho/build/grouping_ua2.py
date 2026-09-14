#!/usr/bin/env python3
"""Build grouping_ua2.pdf: the two halves of an ISO/TS 32005 Table 5 row.

Table 5 states most rows twice, as two rules sharing a clause and differing only
by test number: one governs the parent "when used as a grouping element", the
other "when used as a non-grouping element". Which half applies is not a
property of the type name. ISO 32000-2:2020, 14.8.4.2 lets `Link` sit at block
or inline level, so veraPDF decides per element, in
`GFPDStructElem.getisGrouping`: a type that groups by name always groups, a
block-or-inline type groups when it carries no direct content and no inline
child, and anything else never does.

A coverage scan over 5170 corpus files found both grouping-dependent kinds
unreached: no file anywhere held a `Link`, `Annot`, `Reference` or `FENote` used
as a container. litho therefore decided the question from a static type set and
answered both halves wrongly, reporting test 2 for a `Link` holding one
`Caption` (veraPDF reports nothing) and missing test 1 for a `Link` holding two.

Five `Link` elements sit side by side, none carrying a content item of its own,
so the only thing that varies is what each contains:

| Construct                                   | Rule                    |
| ------------------------------------------- | ----------------------- |
| grouping `Link`, two `Caption` kids         | `Table 5. Link-Caption/1` |
| non-grouping `Link`, `Span` + `H1` kids     | `Table 5. Link-Hn/1`      |
| non-grouping `Link`, `Span` + `Caption` kids | `Table 5. Link-Caption/2` |

The `Span` is what makes the last two non-grouping, and it is the whole point of
planting them beside the first: the same `Caption` kid is legal under a grouping
`Link` and forbidden under a non-grouping one, so a checker that keys on the
type name cannot pass both. Two controls complete the pair, and each is a
construct the wrong half would condemn:

| Control                                | Why it must stay silent                    |
| -------------------------------------- | ------------------------------------------ |
| grouping `Link`, one `Caption` kid     | "at most one" is satisfied                 |
| grouping `Link`, one `H1` kid          | the `Hn` row governs non-grouping use only |

Every firing above was measured on veraPDF 1.30.2 and litho, one construct per
file, before being planted together.

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

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (grouping by use) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))


def elem(struct_type, parent=None, kids=None):
    """One structure element carrying no content item of its own.

    No /K of an integer or an /MCR anywhere below: a direct content item is what
    takes a block-or-inline type out of the grouping role, so planting one would
    silently flip every construct here to its other half.
    """
    d = Dictionary(Type=Name.StructElem, S=Name("/" + struct_type), Pg=page)
    if kids is not None:
        d.K = Array(kids)
    obj = pdf.make_indirect(d)
    if parent is not None:
        obj.P = parent
    return obj


def link_with(children):
    """A Link holding one fresh child per named type, in order."""
    link = elem("Link")
    link.K = Array([elem(t, link) for t in children])
    return link


# Order matters only for readability; each element is judged on its own.
links = [
    link_with(["Caption", "Caption"]),  # grouping, two captions -> t1
    link_with(["Span", "H1"]),  # non-grouping, heading   -> Hn t1
    link_with(["Span", "Caption"]),  # non-grouping, caption   -> t2
    link_with(["Caption"]),  # control: at most one is met
    link_with(["H1"]),  # control: grouping, Hn row does not apply
]

document = elem("Document", kids=links)
for link in links:
    link.P = document

struct_root = pdf.make_indirect(
    Dictionary(
        Type=Name.StructTreeRoot,
        K=Array([document]),
        ParentTree=pdf.make_indirect(Dictionary(Nums=Array([]))),
    )
)
document.P = struct_root

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")

save(pdf, "grouping_ua2.pdf", min_version="2.0")
