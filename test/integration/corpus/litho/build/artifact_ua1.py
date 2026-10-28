#!/usr/bin/env python3
"""Build artifact_ua1.pdf: artifact and real-content marking under PDF/UA-1.

The coverage scan showed ISO 14289-1, clause 7.1 as the largest unreached
cluster in PDF/UA-1. It governs the boundary between real content and
artifacts: an artifact may not sit inside tagged content, tagged content may
not sit inside an artifact, and the catalog may not declare its tagging
suspect.

Three defects sit in one file, and they are independent: two are nesting
errors in the content stream, judged per marked-content sequence, and the
third is a catalog key. A check that reports the first marking error and stops
reports one rule where two are due, and a check that condemns the catalog on
seeing a content error reports three where the file earns them separately.

Two conforming sequences sit beside them: a plain tagged paragraph and a
top-level artifact. Both are must-not-fire controls, because the rule is about
nesting rather than about artifacts or tags as such.

Test 12 of the clause, a structure element missing its `/P`, is deliberately
absent, and litho and veraPDF disagree about it. See ../FIXTURES.md.

Beyond the clause the file carries two independent defects, no XMP packet and
a non-embedded font, so a marking verdict is visibly separate from the
metadata and font clauses.

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


def text(y, s):
    return b"BT /F1 12 Tf 50 %d Td (%s) Tj ET\n" % (y, s)


# Sequence 1 is a conforming tagged paragraph, and sequence 2 a conforming
# top-level artifact: the controls.
#
# The third block nests an artifact inside tagged content, which 7.1 test 1
# forbids, and the fourth nests tagged content inside an artifact, which test 2
# forbids. The two are separate blocks so neither defect can be read as a
# consequence of the other.
content = (
    b"/P <</MCID 0>> BDC\n" + text(730, b"a conforming paragraph") + b"EMC\n"
    b"/Artifact BMC\n" + text(710, b"a conforming artifact") + b"EMC\n"
    b"/P <</MCID 1>> BDC\n"
    + text(690, b"tagged, and below it an artifact")
    + b"/Artifact BMC\n"
    + text(670, b"artifact inside tagged content")
    + b"EMC\n"
    b"EMC\n"
    b"/Artifact BMC\n"
    + text(650, b"artifact, and below it tagged content")
    + b"/P <</MCID 2>> BDC\n"
    + text(630, b"tagged content inside an artifact")
    + b"EMC\n"
    b"EMC\n"
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
paragraphs = [
    pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=mcid)
    )
    for mcid in range(3)
]
document.K = Array(paragraphs)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array(paragraphs)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
# /Suspects declares the tagging unreliable, which ISO 14289-1, 7.1 test 4
# forbids outright. It belongs to no content stream, so it shows a marking
# verdict does not depend on the catalog and vice versa.
pdf.Root.MarkInfo = Dictionary(Marked=True, Suspects=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /Metadata, and the font is not embedded.
save(pdf, "artifact_ua1.pdf", min_version="1.7")
