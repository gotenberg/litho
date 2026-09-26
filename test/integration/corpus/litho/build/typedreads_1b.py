#!/usr/bin/env python3
"""Build typedreads_1b.pdf: the values veraPDF reads again through typed objects.

veraPDF judges a value once as a COS object, reached through the
cross-reference table, and again wherever a typed object of its model wraps it
in a new COS object: an outline item's /Title as a CosTextString, a structure
element's /S as a CosUnicodeName and its /Lang, /Alt and /ActualText, the
catalog's /Lang, each page box as a CosBBox, a stream's /Filter names, the
file specifications of the /EmbeddedFiles name tree. The wrapper has no
identity of its own, so every rule of its type judges it again: the hex string
tests of ISO 19005-1, 6.1.6, and the limits of 6.1.12. No file of any corpus
holds a hex string defect in one of these places, or a box, a filter name or a
structure type past a limit, so no count reached the re-reads; two verapdf
files hold an outline /Title past the string limit, which is where the reading
showed.

Every defective string is distinct, so each row counts on its own:

| Where                                                              | Value     | 6.1.6 at PDF/A-1b |
| ------------------------------------------------------------------ | --------- | ----------------- |
| the catalog's `/Lang`                                              | `<6e6c7>` | t1 x2             |
| the outline dictionary's `/Title`, no item                         | `<414>`   | t1 x1             |
| the first item's `/Title`                                          | `<41424>` | t1 x2             |
| its child's `/Title`                                               | `<434>`   | t1 x2             |
| the second item's `/Title`, an indirect string; `/Next` loops back | `<4G>`    | t2 x2             |
| an item only the outline's `/Last` names                           | `<444>`   | t1 x1             |
| a structure element's `/Lang`, `/Alt`, `/ActualText`               | `<656e7>` | t1 x2 each        |
| its `/E`, which no typed object reads                              | `<434>`   | t1 x1             |
| a `Span` listed twice in its parent's `/K`, its `/Alt`             | `<434>`   | t1 x2             |
| that `Span`'s `/Lang`, an indirect string                          | `<647>`   | t1 x2             |
| a marked-content reference's `/Alt`                                | `<424>`   | t1 x1             |
| the `/Lang` of an `Annot` element a square's `/StructParent` names | `<657>`   | t1 x3             |
| the `/Lang` of a `Form` element a text field's widget names        | `<667>`   | t1 x4             |
| the `/Lang` of an element a second square names                    | `<6a7>`   | t1 x3             |
| the `/Lang` of an element a kid widget with no `/T` names          | `<6b7>`   | t1 x2             |
| an embedded file specification's `/Desc`                           | `<424>`   | t1 x2             |

Each `/Lang` ends on the digit 7, which an odd hex string reads as `70`, so it
decodes to a well-formed language tag (`nlp`, `enp`, `dp`): the hex tests are
all it fails, and the language rules of PDF/A-1a, PDF/A-2a and PDF/UA, whose
counts veraPDF states per read, stay silent.

The limits ride on names and reals: a structure type and a `/RoleMap` key of
130 bytes (6.1.12 t4, twice each), a stream `/Filter` name of 130 bytes (t4,
twice), five page boxes of one page holding a real past 32767 (t2, twice each),
a media box two pages inherit from an intermediate node (t2, once for the node
and once for each page), and a media box whose last element is an indirect
real (t2, once): 6.1.12 t2 counts 14, t4 counts 6.

The counts rest on veraPDF's reach, measured one construct per file before they
were packed. OutlinesHelper walks the items from the outline's /First along
/First and /Next, each once, and never reads the outline dictionary's own
/Title or an item only /Last names. TaggedPDFHelper walks the structure tree
from the root's /K, reading an indirect element once. An annotation reads the
/Lang of the element its /StructParent maps to, and a form field too, down
every /Kids entry holding a /T; the widget that is also the field reads it
twice. A media box is judged for every page it applies to.

qpdf writes every string it knows as a literal, so each defective hex string is
planted as a placeholder literal of the same width and patched as bytes after
the save, which moves no offset. The page tree is built by hand, since
pikepdf's page list pushes an inherited /MediaBox onto each page.

See ../README.md for the authoritative expected firing set.
"""

import pathlib

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save

pdf = pikepdf.new()
patches = []


def hexstr(literal):
    """A placeholder string patched into the hex literal given, bytes and all."""
    placeholder = b"(Q%02d%s)" % (len(patches), b"x" * max(0, len(literal) - 5))
    if len(literal) > len(placeholder):
        raise SystemExit("typedreads_1b: hex literal wider than its placeholder")
    patches.append((placeholder, literal.ljust(len(placeholder))))
    return String(placeholder[1:-1].decode())


LONG = "X" * 130

# Pages: A carries five boxes and the annotations, B and C inherit a media box
# from an intermediate node, D names an indirect real in its media box.
root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 612, 792])


def page(parent, **extra):
    d = Dictionary(Type=Name.Page, Parent=parent, Resources=Dictionary(),
                   Contents=pdf.make_indirect(Stream(pdf, b"0 0 m\n")))
    for key, value in extra.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


def attach(parent, kid, pages=1):
    parent.Kids.append(kid)
    n = parent
    while True:
        n.Count = int(n.Count) + pages
        if "/Parent" not in n:
            break
        n = n.Parent


page_a = page(
    root,
    MediaBox=Array([0, 0, 612, 40000.5]),
    CropBox=Array([0, 0, 612, 40001.5]),
    TrimBox=Array([0, 0, 612, 40002.5]),
    BleedBox=Array([0, 0, 612, 40003.5]),
    ArtBox=Array([0, 0, 612, 40004.5]),
)
attach(root, page_a)
node = pdf.make_indirect(Dictionary(Type=Name.Pages, Kids=Array([]), Count=0, Parent=root,
                                    MediaBox=Array([0, 0, 612, 40005.5])))
attach(root, node, 0)
attach(node, page(node))
attach(node, page(node))
# pikepdf turns an indirect number back into a Python value, so the indirect
# real is planted as an indirect placeholder name and patched like the strings.
REAL_PLACEHOLDER = b"/QIndirectReal"
patches.append((REAL_PLACEHOLDER, b"40006.5".ljust(len(REAL_PLACEHOLDER))))
attach(root, page(root, MediaBox=Array([0, 0, 612, pdf.make_indirect(Name(REAL_PLACEHOLDER.decode()))])))

# Structure tree.
struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))


def element(s, parent, **extra):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + s), P=parent)
    for key, value in extra.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


e_entries = element("P", struct_root, Lang=hexstr(b"<656e7>"), Alt=hexstr(b"<414>"),
                    ActualText=hexstr(b"<424>"), E=hexstr(b"<434>"))
e_long = element(LONG, struct_root)
e_doc = element("Document", struct_root)
span = element("Span", e_doc, Alt=hexstr(b"<434>"), Lang=pdf.make_indirect(hexstr(b"<647>")))
# A /K looping back to the element would do too, but veraPDF's PDF/A-3 file
# specification walk follows it until its stack overflows.
span.K = 0
span.Pg = page_a
e_doc.K = Array([span, span, Dictionary(Type=Name.MCR, Alt=hexstr(b"<424>"))])
e_annot = element("Annot", struct_root, Lang=hexstr(b"<657>"))
e_form = element("Form", struct_root, Lang=hexstr(b"<667>"))
e_annot2 = element("Annot", struct_root, Lang=hexstr(b"<6a7>"))
e_kid = element("Form", struct_root, Lang=hexstr(b"<6b7>"))
struct_root.K = Array([e_entries, e_long, e_doc, e_annot, e_form, e_annot2])
struct_root.RoleMap = Dictionary({"/" + LONG: Name.P})
struct_root.ParentTree = Dictionary(Nums=Array([0, e_annot, 1, e_form, 2, e_annot2, 3, e_kid]))
struct_root.ParentTreeNextKey = 4
pdf.Root.StructTreeRoot = struct_root

# Annotations and form fields reading their elements' /Lang.
square = pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=Name.Square, Rect=Array([0, 0, 10, 10]), F=4,
                                      StructParent=0))
widget = pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=Name.Widget, FT=Name.Tx, T=String("f1"),
                                      Rect=Array([20, 0, 30, 10]), F=4, StructParent=1))
square2 = pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=Name.Square, Rect=Array([40, 0, 50, 10]), F=4,
                                       StructParent=2))
parent_field = pdf.make_indirect(Dictionary(FT=Name.Tx, T=String("parent"), Kids=Array([])))
kid = pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=Name.Widget, Parent=parent_field,
                                   Rect=Array([60, 0, 70, 10]), F=4, StructParent=3))
parent_field.Kids.append(kid)
page_a.Annots = Array([square, widget, square2, kid])
pdf.Root.AcroForm = Dictionary(Fields=Array([widget, parent_field]))

# Outline: the dictionary, a first item with a child, a second item looping
# back to the first, and an item only /Last names.
outlines = pdf.make_indirect(Dictionary(Type=Name.Outlines, Title=hexstr(b"<414>")))
first = pdf.make_indirect(Dictionary(Title=hexstr(b"<41424>"), Parent=outlines))
child = pdf.make_indirect(Dictionary(Title=hexstr(b"<434>"), Parent=first))
second = pdf.make_indirect(Dictionary(Title=pdf.make_indirect(hexstr(b"<4G>")), Parent=outlines, Prev=first))
last_only = pdf.make_indirect(Dictionary(Title=hexstr(b"<444>"), Parent=outlines))
first.Next = second
first.First = child
first.Last = child
second.Next = first
outlines.First = first
outlines.Last = last_only
pdf.Root.Outlines = outlines

pdf.Root.Lang = hexstr(b"<6e6c7>")

# An embedded file whose specification is reached through the name tree.
payload = b"litho corpus attachment"
ef = Stream(pdf, payload)
ef.Type = Name.EmbeddedFile
spec = pdf.make_indirect(Dictionary(Type=Name.Filespec, F=String("b.txt"), UF=String("b.txt"),
                                    Desc=hexstr(b"<424>"), EF=Dictionary(F=pdf.make_indirect(ef))))
pdf.Root.Names = Dictionary(EmbeddedFiles=Dictionary(Names=Array([String("b"), spec])))

# A stream whose /Filter names a filter past the name limit, reached from the
# catalog so the object sweep reads it.
filtered = Stream(pdf, b"abc")
filtered.Filter = Name("/" + "F" * 130)
pdf.Root.Extra = pdf.make_indirect(filtered)

# Deliberately no /Metadata and no output intent.
save(pdf, "typedreads_1b.pdf", min_version="1.4")

path = pathlib.Path(out_path("typedreads_1b.pdf"))
raw = path.read_bytes()
for placeholder, literal in patches:
    if raw.count(placeholder) != 1:
        raise SystemExit("typedreads_1b: expected exactly one %r" % placeholder)
    raw = raw.replace(placeholder, literal)
path.write_bytes(raw)
print("patched %d placeholders into" % len(patches), path)
