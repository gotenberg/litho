#!/usr/bin/env python3
"""Build headings_ua1.pdf: heading structure under PDF/UA-1.

The coverage scan showed ISO 14289-1, 7.4.4 unreached. It asks two things of a
document's headings: no node may hold more than one `H` child, and a document
is either weakly structured with `H` or strongly structured with `H1` to `H6`,
never both.

The second requirement is stated twice, once per object: test 2 against `SEH`
and test 3 against `SEHn`. A document carrying both kinds therefore fails both,
because each kind offends from the other's point of view. That is measured on
both tools, and it is what litho got wrong: it picked a structure "mode" from
whichever heading came first and reported only the other kind, which is half
the defect. Either kind alone agreed; only a document mixing them revealed it.

Three defects sit in one tag tree and they are independent:

| Element | Construct                                  | Rule       |
| ------- | ------------------------------------------ | ---------- |
| Sect1   | two `H` children under one node            | `7.4.4/1`  |
| the `H` elements | a generic `H` beside numbered ones | `7.4.4/2`  |
| H1      | a numbered heading beside generic ones     | `7.4.4/3`  |

`Sect2` holds exactly one `H` child, which is the control for test 1: the
verdict belongs to the node holding two, not to every node holding any.

Beyond the clause the file carries two independent defects, no XMP packet and a
non-embedded font, so a heading verdict is visibly separate from them.

See ../FIXTURES.md for the authoritative expected firing set.
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

LABELS = [
    b"first heading",
    b"second heading",
    b"lone heading",
    b"numbered heading",
    b"body text",
]
content = b"".join(
    b"/P <</MCID %d>> BDC\nBT /F1 12 Tf 50 %d Td (%s) Tj ET\nEMC\n"
    % (i, 740 - i * 24, label)
    for i, label in enumerate(LABELS)
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

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        Lang=String("en-US"),
    )
)

leaves = []
next_mcid = 0


def element(kind, parent):
    """One structure element owning the next marked content sequence."""
    global next_mcid
    made = pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name("/" + kind),
            P=parent,
            Pg=page,
            K=next_mcid,
        )
    )
    next_mcid += 1
    leaves.append(made)
    return made


def section(children):
    s = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.Sect, P=document, Pg=page)
    )
    s.K = Array([element(kind, s) for kind in children])
    return s


# Two generic headings under one node, which the clause caps at one (t1).
sect_two = section(["H", "H"])
# Exactly one generic heading: the control for t1.
sect_one = section(["H"])
# A numbered heading beside the generic ones, which mixes the two structure
# styles and fails both halves of the split (t2 and t3).
numbered = element("H1", document)
body = element("P", document)

document.K = Array([sect_two, sect_one, numbered, body])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array(leaves)])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /Metadata, and the font is not embedded.
save(pdf, "headings_ua1.pdf", min_version="1.7")
