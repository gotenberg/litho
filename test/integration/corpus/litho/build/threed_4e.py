#!/usr/bin/env python3
"""Build threed_4e.pdf: the 3D stream subtypes ISO 19005-4 permits.

PDF/A-4e is the only conformance level that admits a 3D annotation at all, and
ISO 19005-4, 6.1.6.1 test 3 is the only rule that reads the 3D stream it points
at: `Subtype == 'U3D' || Subtype == 'PRC'`. No fixture reached it.

Three 3D annotations sit on the page, each with its own `/3DD` stream:

| Annotation | `/3DD` `/Subtype` | Answers       |
| ---------- | ----------------- | ------------- |
| first      | `/U3D`            | nothing       |
| second     | `/PRC`            | nothing       |
| third      | `/LithoNot3D`     | `6.1.6.1/3`   |

Both permitted values are planted as controls, not just one: the test is a
two-way disjunction, and a check that hardcodes only `U3D` passes the file
anyway unless a `PRC` sits beside it. The defective stream is otherwise
identical to the two conforming ones, so a verdict attached to the wrong stream
answers on a control.

Every annotation carries an appearance stream and a `/Contents` string, so
ISO 19005-4, 6.3.3 stays silent and the 6.1.6.1 verdict stands alone. The
annotations are also the reason the file has to be read at PDF/A-4e: at every
other profile a 3D annotation is a forbidden subtype, which is a different
clause on a different object.

An unembedded Helvetica paints the page, and there is neither `/Metadata` nor
an output intent, so three further clauses answer from elsewhere in the
specification. A checker that stops at its first verdict reports one rule where
four are due.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

# ISO 19005-4, 6.1.6.1 t3 names both, so both are planted.
PERMITTED = ("U3D", "PRC")
FORBIDDEN = "LithoNot3D"

font = pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.Type1,
    BaseFont=Name("/Helvetica"), Encoding=Name.WinAnsiEncoding))

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=Stream(pdf, b"BT /F1 12 Tf 50 720 Td (three dimensional) Tj ET\n")))
pdf.pages.append(pikepdf.Page(page))


def artwork(subtype):
    """One 3D stream, ISO 32000-2, 13.6.3. The payload is opaque to the test."""
    stream = Stream(pdf, b"litho corpus 3D artwork")
    stream.Type = Name("/3D")
    stream.Subtype = Name("/" + subtype)
    return pdf.make_indirect(stream)


def annotation(subtype, index):
    """A 3D annotation with an appearance, so only the stream subtype answers."""
    appearance = Stream(pdf, b"0.5 0.5 0.5 rg 0 0 150 50 re f\n")
    appearance.Type = Name.XObject
    appearance.Subtype = Name.Form
    appearance.BBox = Array([0, 0, 150, 50])
    return pdf.make_indirect(Dictionary(
        Type=Name.Annot,
        Subtype=Name("/3D"),
        Rect=Array([50, 400 + index * 70, 200, 450 + index * 70]),
        F=4,
        Contents=String("3D artwork %s" % subtype),
        AP=Dictionary(N=pdf.make_indirect(appearance)),
        **{"3DD": artwork(subtype)}))


page.Annots = Array([annotation(s, i)
                     for i, s in enumerate(PERMITTED + (FORBIDDEN,))])

# Deliberately no /Metadata and no output intent.
save(pdf, "threed_4e.pdf", min_version="2.0")
