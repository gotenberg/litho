#!/usr/bin/env python3
"""Build structkinds_ua2.pdf: five ISO 14289-2 structure-element kinds at once.

The coverage scan left five PDF/UA-2 structure tests unreached, one per kind of
element. They are independent objects of the same walk, so planting them
together is the masking probe: a checker that reports the first structural
complaint and stops reports one rule where five are due.

| Element                                | Rule           |
| -------------------------------------- | -------------- |
| a root holding two `Document` elements | `8.2.5.2/1`    |
| `Ruby` whose kids are RB, RP, RT       | `8.2.5.23/1`   |
| `Warichu` whose kids are WP, WT        | `8.2.5.24/1`   |
| `LI` holding a marked-content item     | `8.2.5.25/2`   |
| `math` whose parent is a `P`           | `8.2.5.29/1`   |

The root defect is the one that interacts, since 8.2.5.2 t1 judges the very
kids the other four hang from. Two `Document` elements break it while leaving
both subtrees walkable, which is measured rather than assumed: the `Warichu`
pair lives under the SECOND `Document` on purpose, so a walk that reads only
the first root kid loses 8.2.5.24 t1 outright. The construct also fires
ISO/TS 32005 `Table 5. StructTreeRoot-Document`, which counts `Document` kids
rather than comparing the whole kid sequence.

Controls sit beside every defect, because each of these tests judges a shape
rather than a kind: a `Ruby` of RB, RT and another of RB, RP, RT, RP, a
`Warichu` of WP, WT, WP, an `LI` holding `Lbl` and `LBody` beside one holding
`LBody` alone, and a `math` element parented by a `Formula`. veraPDF reports a
rule once however many elements break it, so rule-set agreement alone cannot
see these; the per-rule failed-check counts are what prove them silent.

The MathML namespace is load-bearing and was measured on both tools. veraPDF
decides `SEMathMLStructElem` by namespace, not by type name: the same element
declared in the PDF 2.0 namespace, or declaring no namespace at all, is not a
math element to it and reports 8.2.4 t1 instead. So the `math` elements here
bind to http://www.w3.org/1998/Math/MathML deliberately, and the namespace is
declared in the root's `/Namespaces`.

The font is not embedded, so a structure verdict is visibly separate from the
font clause.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
MATHML_NS = "http://www.w3.org/1998/Math/MathML"

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
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
nsmath = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(MATHML_NS)))
struct_root = pdf.make_indirect(
    Dictionary(Type=Name.StructTreeRoot, Namespaces=Array([ns20, nsmath]))
)

# 8.2.5.2 t1: the root shall hold a single Document as its only child. Two of
# them break it without hiding anything: both subtrees stay reachable, which
# the Warichu pair under the second one proves.
doc_a = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ns20,
        Lang=String("en-US"),
    )
)
doc_b = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ns20,
        Lang=String("en-US"),
    )
)

leaves = []
tags = []


def leaf(kind, parent, namespace=None):
    """One element owning the next marked content sequence on the page.

    The sequence is emitted after the tree is built, so every MCID has exactly
    one owner: an unowned one fires 8.2.2 t1 and drowns out the structure
    verdicts this file exists to separate.
    """
    mcid = len(tags)
    tags.append(kind)
    made = pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name("/" + kind),
            P=parent,
            NS=namespace or ns20,
            Pg=page,
            K=mcid,
        )
    )
    leaves.append(made)
    return made


def branch(kind, parent, build, namespace=None, attrs=None):
    """One element whose children are structure elements rather than content."""
    d = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + kind),
        P=parent,
        NS=namespace or ns20,
        Pg=page,
    )
    if attrs is not None:
        d.A = Dictionary(**attrs)
    made = pdf.make_indirect(d)
    made.K = Array(build(made))
    return made


LIST_ATTRS = {"O": Name("/List"), "ListNumbering": Name("/Unordered")}

# 8.2.5.23 t1: a Ruby of RB, RP, RT. The clause admits RB, RT and the
# four-element RB, RP, RT, RP, so a three-element run is a real subsequence of
# the longer form rather than an unrelated shape.
ruby_bad = branch(
    "P",
    doc_a,
    lambda p: [
        branch(
            "Ruby",
            p,
            lambda r: [leaf("RB", r), leaf("RP", r), leaf("RT", r)],
        )
    ],
)

# Both conforming forms, so the check is visibly about the sequence rather
# than about Ruby elements as such.
ruby_ok2 = branch(
    "P",
    doc_a,
    lambda p: [branch("Ruby", p, lambda r: [leaf("RB", r), leaf("RT", r)])],
)
ruby_ok4 = branch(
    "P",
    doc_a,
    lambda p: [
        branch(
            "Ruby",
            p,
            lambda r: [leaf("RB", r), leaf("RP", r), leaf("RT", r), leaf("RP", r)],
        )
    ],
)

# 8.2.5.25 t2: real content inside an LI shall be wrapped in Lbl or LBody. This
# LI owns a marked content sequence directly.
list_bad = branch(
    "L", doc_a, lambda l: [leaf("LI", l)], attrs=LIST_ATTRS
)

# The control: one LI wrapping both, one wrapping LBody alone. /ListNumbering
# is present because 8.2.5.25 t1 asks for it once any LI carries a label, and
# that rule is not what this file is testing.
list_ok = branch(
    "L",
    doc_a,
    lambda l: [
        branch("LI", l, lambda li: [leaf("Lbl", li), leaf("LBody", li)]),
        branch("LI", l, lambda li: [leaf("LBody", li)]),
    ],
    attrs=LIST_ATTRS,
)

# 8.2.5.29 t1: a math element shall be a child of Formula. This one hangs off a
# paragraph. The MathML namespace is what makes it a math element at all.
math_bad = branch("P", doc_a, lambda p: [leaf("math", p, namespace=nsmath)])

# The control: the same element under a Formula.
math_ok = branch(
    "Formula", doc_a, lambda f: [leaf("math", f, namespace=nsmath)]
)

# 8.2.5.24 t1: a Warichu shall be the three-element run WP, WT, WP. This one
# stops at two. It lives under the second Document so that a walk which reads
# only the first root kid reports nothing for the clause.
wari_bad = branch(
    "P",
    doc_b,
    lambda p: [branch("Warichu", p, lambda w: [leaf("WP", w), leaf("WT", w)])],
)

# The control, also under the second Document.
wari_ok = branch(
    "P",
    doc_b,
    lambda p: [
        branch("Warichu", p, lambda w: [leaf("WP", w), leaf("WT", w), leaf("WP", w)])
    ],
)

doc_a.K = Array([ruby_bad, ruby_ok2, ruby_ok4, list_bad, list_ok, math_bad, math_ok])
doc_b.K = Array([wari_bad, wari_ok])
struct_root.K = Array([doc_a, doc_b])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array(leaves)])))
struct_root.ParentTreeNextKey = 1

page.Contents = Stream(
    pdf,
    b"".join(
        b"/%s <</MCID %d>> BDC\nBT /F1 9 Tf 50 %d Td (%s %d) Tj ET\nEMC\n"
        % (tag.encode("ascii"), i, 740 - i * 26, tag.encode("ascii"), i)
        for i, tag in enumerate(tags)
    ),
)

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">structure element kinds</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>2</pdfuaid:part>
   <pdfuaid:rev>2024</pdfuaid:rev>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "structkinds_ua2.pdf", min_version="2.0")
