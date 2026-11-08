#!/usr/bin/env python3
"""Build fenote_ua2.pdf: footnote and endnote structure under PDF/UA-2.

The coverage scan showed ISO 14289-2, 8.2.5.14 entirely unreached, and PDF/UA-2
as the weakest profile in the corpus. The clause governs how a document marks
up notes: PDF 2.0 replaces the PDF 1.7 `Note` type with `FENote`, requires the
referring content and the note to point at each other through `/Ref`, and
constrains the note's `/NoteType`.

All four tests are independent and are planted together, which is the masking
probe: a check that reports the first offending note and stops reports one rule
where four are due.

The two `/Ref` directions were measured on both tools rather than read out of
the clause, which names neither:

| construct                                  | fires |
| ------------------------------------------ | ----- |
| referrer points at the note, note silent   | t2    |
| note points at the referrer, referrer silent | t3  |
| both point at each other                   | none  |
| neither points at anything                 | none  |

So a bare `FENote` with no `/Ref` at all conforms, and only a half-built
association fails. Both of those are planted as must-not-fire controls.

Test 1 turns on the namespace, not the type name: a `Note` bound to the PDF 1.7
namespace fires, while the same element claimed in the PDF 2.0 namespace
resolves elsewhere and reports 8.2.4 t1 instead. The legacy note here is
therefore bound to the 1.7 namespace deliberately.

Beyond the note cluster the file carries two independent defects, no XMP packet
and a non-embedded font, so a structure verdict is visibly separate from the
metadata and font clauses.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF17_NS = "http://iso.org/pdf/ssn"
PDF20_NS = "http://iso.org/pdf2/ssn"

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# One marked content sequence per structure element: six referring paragraphs
# and the six notes they pair with.
parts = []
for i in range(12):
    parts.append(
        b"/P <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (item %d) Tj ET\nEMC\n"
        % (i, 740 - i * 24, i)
    )
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"".join(parts)),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

ns17 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF17_NS)))
ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(
    Dictionary(Type=Name.StructTreeRoot, Namespaces=Array([ns20, ns17]))
)
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ns20,
        Lang=String("en-US"),
    )
)


def para(mcid):
    """One referring paragraph, in the PDF 2.0 namespace."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=mcid
        )
    )


def note(mcid, kind="FENote", namespace=None, note_type=None):
    """One note element. kind FENote is the PDF 2.0 replacement for Note."""
    d = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + kind),
        P=document,
        NS=namespace or ns20,
        Pg=page,
        K=mcid,
    )
    if note_type is not None:
        d.A = Dictionary(O=Name("/FENote"), NoteType=Name("/" + note_type))
    return pdf.make_indirect(d)


# N1: the legacy Note type, bound to the PDF 1.7 namespace, which PDF/UA-2
# forbids outright (t1).
p1, n1 = para(0), note(1, kind="Note", namespace=ns17)

# N2: the paragraph points at the note and the note stays silent (t2).
p2, n2 = para(2), note(3, note_type="Footnote")
p2.Ref = Array([n2])

# N3: the note points at the paragraph and the paragraph stays silent (t3).
p3, n3 = para(4), note(5, note_type="Endnote")
n3.Ref = Array([p3])

# N4: a fully wired association whose /NoteType is none of Footnote, Endnote
# or None (t4). Its /Ref pair is complete, so t2 and t3 stay silent on it and
# the NoteType verdict stands alone.
p4, n4 = para(6), note(7, note_type="Sidenote")
p4.Ref = Array([n4])
n4.Ref = Array([p4])

# N5 conforms in every respect, so a firing against it is over-suppression.
p5, n5 = para(8), note(9, note_type="Footnote")
p5.Ref = Array([n5])
n5.Ref = Array([p5])

# N6 refers to nothing and nothing refers to it, which conforms: the clause
# constrains half-built associations, not absent ones.
p6, n6 = para(10), note(11, note_type="None")

document.K = Array([p1, n1, p2, n2, p3, n3, p4, n4, p5, n5, p6, n6])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array(list(document.K))]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /Metadata: the XMP clauses stay reachable beside the notes.
save(pdf, "fenote_ua2.pdf", min_version="2.0")
