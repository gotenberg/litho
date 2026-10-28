#!/usr/bin/env python3
"""Build annotkinds_ua2.pdf: annotation subtypes under PDF/UA-2.

The coverage scan showed ISO 14289-2 8.9.2.4 almost wholly unreached: it judges
annotations subtype by subtype, and each of its tests is scoped to one veraPDF
object, so a file reaches them only by carrying that exact annotation kind.

Eleven annotations sit on one page, eight defective and three conforming, which
is the masking probe: the clause splits one idea, "an annotation must be
describable or be an artifact", across nine independent tests, so a check that
reports the first offending annotation and stops reports one rule where eight
are due, and a check that condemns every annotation of a kind fires on the
conforming neighbours.

| Annotation                             | Rule           |
| -------------------------------------- | -------------- |
| `Sound`, any                           | `8.9.2.4.11/1` |
| `TrapNet`, any                         | `8.9.2.4.15/1` |
| `Screen` with no `/Contents`           | `8.9.2.4.12/1` |
| `3D` with no `/Contents`               | `8.9.2.4.19/1` |
| `RichMedia` with no `/Contents`        | `8.9.2.4.19/2` |
| `Ink` with no `/Contents`              | `8.9.2.4.8/1`  |
| `Stamp` with neither `/Name` nor `/Contents` | `8.9.2.4.7/1` |
| `PrinterMark` inside the structure tree | `8.9.2.4.14/1` |
| `Screen` with `/Contents`              | silent         |
| `Ink` with `/Contents`                 | silent         |
| `Stamp` with `/Name`                   | silent         |

Two of the eleven need no defect at all. `Sound` and `TrapNet` are forbidden
outright, so their tests read simply `false`: the annotation's presence is the
violation. They are the reason the file cannot be reduced to "annotations
missing a description".

`PrinterMark` is the opposite shape. Its rule is satisfied when the annotation
is outside the structure tree or marked as an artifact, so reaching it means
putting the annotation *into* the tree, wired the way a real one is: a
`/StructParent` on the annotation, an `/OBJR` in the enclosing element's `/K`,
and a `ParentTree` entry mapping the number back. Get that wiring wrong and the
annotation looks unparented, which silences the test rather than firing it.

The three controls are each the conforming twin of a defect beside them, so a
firing against any of them is over-suppression rather than a missing case. The
`Stamp` control uses `/Name` rather than `/Contents` deliberately: the test
accepts either, and only the `/Name` branch is otherwise unexercised.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"

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
        Contents=Stream(
            pdf,
            b"/P <</MCID 0>> BDC\n"
            b"BT /F1 12 Tf 50 740 Td (annotation subtypes) Tj ET\nEMC\n",
        ),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(
    Dictionary(Type=Name.StructTreeRoot, Namespaces=Array([ns20]))
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


def appearance(w=20, h=20):
    s = Stream(pdf, b"0 0 0 rg 0 0 %d %d re f\n" % (w, h))
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, w, h])
    return pdf.make_indirect(s)


PRINT_FLAG = 4
annots = []


def annot(subtype, y, **extra):
    """One annotation carrying a print flag and an appearance stream."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name("/" + subtype),
        Rect=Array([50, y, 70, y + 20]),
        F=PRINT_FLAG,
        AP=Dictionary(N=appearance()),
    )
    for k, v in extra.items():
        d[Name("/" + k)] = v
    made = pdf.make_indirect(d)
    annots.append(made)
    return made


# Forbidden outright: the presence of either annotation is the violation.
annot("Sound", 700)
annot("TrapNet", 670)

# Missing the description each subtype requires.
annot("Screen", 640)
annot("3D", 610)
annot("RichMedia", 580)
annot("Ink", 550, InkList=Array([Array([50, 550, 70, 570])]))

# A rubber stamp offering neither of the two entries its test accepts.
annot("Stamp", 520)

# The conforming twins. Each is the same subtype as a defect above, so a firing
# here is over-suppression rather than a missing case.
annot("Screen", 490, Contents=String("a described screen annotation"))
annot("Ink", 460, Contents=String("a described ink annotation"),
      InkList=Array([Array([50, 460, 70, 480])]))
annot("Stamp", 430, Name=Name.Approved)

# A printer's mark must be an artifact or sit outside the structure tree. This
# one is inside it, which is what the test judges, so it needs the full wiring:
# /StructParent here, an /OBJR below, and a ParentTree entry mapping it back.
printer_mark = annot("PrinterMark", 400, StructParent=1)

paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)
mark_holder = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.P,
        P=document,
        NS=ns20,
        Pg=page,
        K=Array([Dictionary(Type=Name.OBJR, Obj=printer_mark)]),
    )
)

page.Annots = Array(annots)

document.K = Array([paragraph, mark_holder])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([paragraph]), 1, mark_holder]))
)
struct_root.ParentTreeNextKey = 2

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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">annotation subtypes</rdf:li></rdf:Alt></dc:title>
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

save(pdf, "annotkinds_ua2.pdf", min_version="2.0")
