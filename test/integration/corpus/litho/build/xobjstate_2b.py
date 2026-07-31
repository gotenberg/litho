#!/usr/bin/env python3
"""Build xobjstate_2b.pdf: Form XObject keys, blend modes and page actions.

The coverage scan showed four ISO 19005-2 rules unreached that judge different
parts of a page: what a Form XObject dictionary may carry, what a graphics state
may name as a blend mode, whether a page may act on being opened, and whether
two Separations agreeing on a colorant name agree on how to render it.

| Construct                                        | Rule         |
| ------------------------------------------------ | ------------ |
| a Form XObject carrying `/OPI`                   | `6.2.9/1`    |
| an `ExtGState` naming a blend mode outside PDF   | `6.2.10/1`   |
| a page carrying `/AA`                            | `6.5.2/2`    |
| two Separations sharing a colorant, disagreeing  | `6.2.4.4/2`  |

Four independent axes in one file, which is the masking probe: a check that
abandons a page after its first complaint reports one rule where four are due.

Each defect has a conforming neighbour, because every one of these tests turns
on a value rather than on a kind: a second Form XObject with no `/OPI`, a second
graphics state naming `/Multiply`, and a third Separation whose colorant name is
its own. A firing against any of them is over-suppression.

`6.2.9/1` reads `(Subtype2 == null || Subtype2 != "PS") && containsPS == false
&& containsOPI == false`, so three different keys break one test. Only `/OPI` is
planted: the other two would collapse onto the same rule and prove nothing extra,
while `/OPI` is the one a real producer emits.

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


def form(name, **extra):
    """A Form XObject painting a small square. Extra keys ride on the dict."""
    stream = Stream(pdf, b"0 0 1 rg 0 0 20 20 re f\n")
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 20, 20])
    stream.Resources = Dictionary()
    for k, v in extra.items():
        setattr(stream, k, v)
    return pdf.make_indirect(stream)


# 6.2.9 t1: /OPI is forbidden on a Form XObject. The neighbour carries none.
form_opi = form("FmO", OPI=Dictionary(**{"1.3": Dictionary(F=String("linked.tif"))}))
form_ok = form("FmC")

# 6.2.10 t1: the blend mode must be one PDF defines. /Foo is not, and the
# neighbour names /Multiply, which is.
gs_bad = pdf.make_indirect(Dictionary(Type=Name.ExtGState, BM=Name("/Foo")))
gs_ok = pdf.make_indirect(Dictionary(Type=Name.ExtGState, BM=Name.Multiply))


def tint(expr):
    """A PostScript tint transform from one input to three RGB components."""
    stream = Stream(pdf, expr.encode("ascii"))
    stream.FunctionType = 4
    stream.Domain = Array([0, 1])
    stream.Range = Array([0, 1, 0, 1, 0, 1])
    return pdf.make_indirect(stream)


# 6.2.4.4 t2: two Separations naming the same colorant shall render it the same
# way. These disagree, one painting it red and the other green. The third names
# a colorant of its own, so it has nothing to disagree with.
sep_a = pdf.make_indirect(
    Array([Name.Separation, Name("/Spot"), Name.DeviceRGB, tint("{ dup 0 mul exch dup 0 mul exch 0 mul }")])
)
sep_b = pdf.make_indirect(
    Array([Name.Separation, Name("/Spot"), Name.DeviceRGB, tint("{ dup 0 mul exch dup 1 mul exch 0 mul }")])
)
sep_ok = pdf.make_indirect(
    Array([Name.Separation, Name("/Solo"), Name.DeviceRGB, tint("{ dup 0 mul exch dup 0 mul exch 1 mul }")])
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            XObject=Dictionary(FmO=form_opi, FmC=form_ok),
            ExtGState=Dictionary(GsB=gs_bad, GsC=gs_ok),
            ColorSpace=Dictionary(CsA=sep_a, CsB=sep_b, CsC=sep_ok),
        ),
        Contents=Stream(
            pdf,
            b"BT /F1 12 Tf 50 700 Td (form keys, blend modes and actions) Tj ET\n"
            b"q /GsB gs 1 0 0 1 60 600 cm /FmO Do Q\n"
            b"q /GsC gs 1 0 0 1 120 600 cm /FmC Do Q\n"
            b"q /CsA cs 1 scn 60 500 20 20 re f Q\n"
            b"q /CsB cs 1 scn 100 500 20 20 re f Q\n"
            b"q /CsC cs 1 scn 140 500 20 20 re f Q\n",
        ),
        # 6.5.2 t2: a page shall carry no additional-actions dictionary.
        AA=Dictionary(O=Dictionary(S=Name.Named, N=Name.NextPage)),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata, both independent of the
# clauses above.
save(pdf, "xobjstate_2b.pdf", min_version="1.7")
