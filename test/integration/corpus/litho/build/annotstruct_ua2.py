#!/usr/bin/env python3
"""Build annotstruct_ua2.pdf: annotations judged by their place in the tree.

`annotkinds_ua2.pdf` covers the annotation rules that turn on the subtype alone.
This file covers the rest of ISO 14289-2 clause 8.9, which turn on where the
annotation sits: almost every test here is satisfied by `structParentType ==
null`, so an annotation only reaches the test by being wired into the structure
tree, and reaching a test is the hard part rather than failing it.

Wiring means three things agreeing: a `/StructParent` on the annotation, an
`/OBJR` in the enclosing element's `/K`, and a `ParentTree` entry mapping the
number back to that element. Miss one and the annotation reads as unparented,
which silences the test rather than firing it.

| Annotation                                        | Rule            |
| ------------------------------------------------- | --------------- |
| `Link` under a `/P`                               | `8.2.5.20/1`    |
| `NoView` set without `ToggleNoView`               | `8.9.2.2/2`     |
| `Square` under a `/P`                             | `8.9.2.3/1`     |
| `Square` whose `/RC` differs from `/Contents`     | `8.9.2.3/2`     |
| `Popup` inside the tree                           | `8.9.2.4.9/1`   |
| `FileAttachment` with `/FS`, no `/AFRelationship` | `8.9.2.4.10/1`  |
| zero-area `Widget` inside the tree                | `8.9.2.4.13/1`  |
| `Watermark` under a `/P`                          | `8.9.2.4.16/1`  |
| `/Contents` against the element's differing `/Alt` | `8.9.4.2/1`    |
| a field widget with `/AA` and no `/Contents`      | `8.10.2.3/2`    |

Two of these are placed where measurement put them rather than where the entry
name suggests. `/AFRelationship` is a file specification entry, so an
annotation-level one leaves `8.9.2.4.10/1` firing and does not make a control.
`8.9.4.2/1` compares the annotation's `/Contents` with the `/Alt` of the element
enclosing it, so an annotation-level `/Alt` never reaches the test at all.

The enclosing element's type is the discriminator for four of these, so the file
carries elements of several types on purpose: a `/Link` element, an `/Annot`
element and plain `/P` elements. An annotation moved between them changes verdict
without changing one byte of the annotation itself, which is why a check that
reads only the annotation dictionary cannot pass this file.

Controls sit beside the defects wherever the test turns on an entry rather than
on position: a `Link` under a `/Link` element, a `Square` under an `/Annot`
element, a `Square` whose `/RC` matches its `/Contents`, a `FileAttachment`
carrying `/AFRelationship`, an annotation whose `/Alt` equals its `/Contents`,
a full-area widget, and a `Popup` left outside the tree. veraPDF reports a rule
once however many annotations break it, so rule-set agreement alone cannot see
these; the per-rule failed-check counts are what prove them.

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
            b"BT /F1 12 Tf 50 750 Td (annotations in the structure tree) Tj ET\nEMC\n",
        ),
        StructParents=0,
        Tabs=Name.S,
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

paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)

annots = []
doc_kids = [paragraph]
# ParentTree keys start after the page's own StructParents of 0.
nums = [0, Array([paragraph])]
next_key = 1


def appearance(w=20, h=20):
    s = Stream(pdf, b"0 0 0 rg 0 0 %d %d re f\n" % (max(w, 1), max(h, 1)))
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, max(w, 1), max(h, 1)])
    return pdf.make_indirect(s)


def annot(subtype, rect, **extra):
    """One annotation carrying a print flag and an appearance stream."""
    w = float(rect[2]) - float(rect[0])
    h = float(rect[3]) - float(rect[1])
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name("/" + subtype),
        Rect=Array(rect),
        F=4,
        AP=Dictionary(N=appearance(int(w), int(h))),
    )
    for k, v in extra.items():
        d[Name("/" + k)] = v
    made = pdf.make_indirect(d)
    annots.append(made)
    return made


def enclose(kind, target, **extra):
    """Wire target into the tree under a new element of the given type.

    The three parts have to agree: the /StructParent written here, the /OBJR in
    the element's /K, and the ParentTree entry mapping the number back. Extra
    keys land on the element rather than the annotation, which is where /Alt
    belongs.
    """
    global next_key
    key = next_key
    next_key += 1
    fields = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + kind),
        P=document,
        NS=ns20,
        Pg=page,
        K=Array([Dictionary(Type=Name.OBJR, Obj=target)]),
    )
    for k, v in extra.items():
        fields[Name("/" + k)] = v
    element = pdf.make_indirect(fields)
    target.StructParent = key
    doc_kids.append(element)
    nums.extend([key, element])
    return element


def y(row):
    """Row coordinates, laid out downward so nothing overlaps."""
    top = 720 - row * 30
    return [50, top, 90, top + 20]


# 8.2.5.20 t1: a Link whose enclosing element is neither Link nor Reference.
link_bad = annot("Link", y(0), A=Dictionary(S=Name.GoTo, D=Array([page, Name.Fit])))
enclose("P", link_bad)

# The control: the same annotation under a /Link element.
link_ok = annot("Link", y(1), A=Dictionary(S=Name.GoTo, D=Array([page, Name.Fit])))
enclose("Link", link_ok)

# 8.2.5.20 t2: two Links under one /Link element aiming at different targets.
link_pair_a = annot("Link", y(2), A=Dictionary(S=Name.GoTo, D=Array([page, Name.Fit])))
link_pair_b = annot(
    "Link", y(3), A=Dictionary(S=Name.GoTo, D=Array([page, Name.XYZ, 0, 700, 0]))
)
pair_element = enclose("Link", link_pair_a)
link_pair_b.StructParent = next_key
pair_element.K.append(Dictionary(Type=Name.OBJR, Obj=link_pair_b))
nums.extend([next_key, pair_element])
next_key += 1

# 8.9.2.2 t2: NoView set, ToggleNoView clear. Print | NoView is 4 | 32.
noview = annot("Square", y(4), F=4 | 32, Contents=String("a hidden square"))
enclose("Annot", noview)

# The control: the same flags plus ToggleNoView, which the test permits.
noview_ok = annot(
    "Square", y(5), F=4 | 32 | 256, Contents=String("a toggling square")
)
enclose("Annot", noview_ok)

# 8.9.2.3 t1: a markup annotation whose enclosing element is not /Annot.
markup_bad = annot("Square", y(6), Contents=String("a misplaced square"))
enclose("P", markup_bad)

# 8.9.2.3 t2: /RC present and disagreeing with /Contents.
RC_HTML = (
    "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'>"
    "<p>something else entirely</p></body>"
)
rc_bad = annot(
    "Square", y(7), Contents=String("the plain text"), RC=String(RC_HTML)
)
enclose("Annot", rc_bad)

# The control: /RC whose text matches /Contents.
rc_ok = annot(
    "Square",
    y(8),
    Contents=String("agreeing text"),
    RC=String(
        "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'>"
        "<p>agreeing text</p></body>"
    ),
)
enclose("Annot", rc_ok)

# 8.9.2.4.9 t1: a Popup inside the tree. Its parent markup annotation is the
# one carrying the /RC control above, so the pair is well formed apart from the
# structure membership the test judges.
popup_bad = annot("Popup", y(9), Parent=rc_ok, Open=False)
enclose("Annot", popup_bad)

# The control: a Popup left outside the tree entirely, which the test permits.
annot("Popup", y(10), Parent=markup_bad, Open=False)

# 8.9.2.4.10 t1: a file attachment carrying /FS but no /AFRelationship.
embedded = Stream(pdf, b"attachment payload\n")
embedded.Type = Name.EmbeddedFile
embedded.Subtype = Name("/text#2Fplain")
embedded = pdf.make_indirect(embedded)


def filespec(name, relationship=None):
    """A file specification, optionally declaring how the file relates.

    ISO 32000-2 defines /AFRelationship on the file specification dictionary,
    not on the annotation, and veraPDF reads it from there: an annotation-level
    /AFRelationship leaves the test firing.
    """
    d = Dictionary(
        Type=Name.Filespec,
        F=String(name),
        UF=String(name),
        EF=Dictionary(F=embedded),
    )
    if relationship is not None:
        d.AFRelationship = relationship
    return pdf.make_indirect(d)


attach_bad = annot(
    "FileAttachment", y(11), FS=filespec("plain.txt"), Contents=String("an attachment")
)
enclose("Annot", attach_bad)

# The control: the same annotation whose file spec declares the relationship.
attach_ok = annot(
    "FileAttachment",
    y(12),
    FS=filespec("related.txt", Name.Supplement),
    Contents=String("a described attachment"),
)
enclose("Annot", attach_ok)

# 8.9.2.4.13 t1: a widget of zero width and zero height inside the tree.
zero_widget = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 380, 50, 380]),
        F=4,
        FT=Name.Btn,
        T=String("zeroarea"),
        Contents=String("a zero area widget"),
        AP=Dictionary(N=appearance(1, 1)),
    )
)
annots.append(zero_widget)
enclose("Form", zero_widget)

# 8.10.2.3 t2: a field widget with additional actions and no /Contents.
aa_widget = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 340, 110, 360]),
        F=4,
        FT=Name.Btn,
        T=String("acts"),
        AA=Dictionary(U=Dictionary(S=Name.Named, N=Name.NextPage)),
        AP=Dictionary(N=appearance(60, 20)),
    )
)
annots.append(aa_widget)
enclose("Form", aa_widget)

# The control: additional actions beside a /Contents that describes them.
aa_widget_ok = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 300, 110, 320]),
        F=4,
        FT=Name.Btn,
        T=String("describedacts"),
        Contents=String("a described acting widget"),
        AA=Dictionary(U=Dictionary(S=Name.Named, N=Name.NextPage)),
        AP=Dictionary(N=appearance(60, 20)),
    )
)
annots.append(aa_widget_ok)
enclose("Form", aa_widget_ok)

# 8.9.2.4.16 t1: a watermark whose enclosing element is not /Annot.
watermark = annot("Watermark", y(15), Contents=String("a watermark"))
enclose("P", watermark)

# 8.9.4.2 t1: the annotation's /Contents against the /Alt of the element that
# encloses it. The two describe the same thing to different consumers, so they
# have to agree. /Alt rides on the structure element, not on the annotation; an
# annotation-level /Alt is invisible to the test.
alt_bad = annot("Square", y(16), Contents=String("one description"))
enclose("Annot", alt_bad, Alt=String("a different description"))

# The control: the two agreeing exactly.
alt_ok = annot("Square", y(17), Contents=String("same words"))
enclose("Annot", alt_ok, Alt=String("same words"))

page.Annots = Array(annots)
pdf.Root.AcroForm = Dictionary(
    Fields=Array([zero_widget, aa_widget, aa_widget_ok]),
    DA=String("/Helv 0 Tf 0 g"),
    DR=Dictionary(Font=Dictionary(Helv=font)),
)

document.K = Array(doc_kids)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array(nums)))
struct_root.ParentTreeNextKey = next_key

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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">annotations in the tree</rdf:li></rdf:Alt></dc:title>
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

save(pdf, "annotstruct_ua2.pdf", min_version="2.0")
