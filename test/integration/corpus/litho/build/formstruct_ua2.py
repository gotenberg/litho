#!/usr/bin/env python3
"""Build formstruct_ua2.pdf: widget annotations and Form structure under PDF/UA-2.

The coverage scan showed ISO 14289-2, 8.10.1 as the last unreached cluster in
PDF/UA-2 outside the deliberately skipped ones. It ties interactive form fields
to the structure tree: every field widget belongs inside a `Form` structure
element, a `Form` element holds at most one widget, and XFA is forbidden
outright.

Three defects sit in one file and are independent: two are structure
associations, judged per annotation and per `Form` element, and the third is a
key on the AcroForm dictionary. A check that reports the first misplaced widget
and stops reports one rule where two are due, and a check that condemns the
AcroForm on seeing a structure error reports three where the file earns them
separately.

Two controls sit beside them. `FormOK` encloses exactly one widget, which is
the shape the clause asks for, and a widget with no `/StructParent` at all is
outside the structure tree, which test 1 permits explicitly: its predicate
passes when the structure parent type is null. That second control matters
because the natural way to write the check, "every widget must sit under a
Form", fires on it.

Widgets reach the structure tree through `/StructParent` and an `/OBJR` entry
in the element's `/K`, so the parent tree maps each widget's number back to the
element that encloses it. Getting that wiring wrong makes every widget look
unparented, which would fire test 1 everywhere and mask the rest.

See ../README.md for the authoritative expected firing set.
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
            b"BT /F1 12 Tf 50 740 Td (interactive form) Tj ET\nEMC\n",
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


def appearance():
    s = Stream(pdf, b"0 0 0 rg 0 0 20 20 re f\n")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(s)


def widget(name, y, struct_parent=None):
    """One text field widget. struct_parent=None leaves it out of the tree."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, y, 70, y + 20]),
        F=4,
        FT=Name.Tx,
        T=String(name),
        AP=Dictionary(N=appearance()),
    )
    if struct_parent is not None:
        d.StructParent = struct_parent
    return pdf.make_indirect(d)


def element(kind, kids):
    e = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name("/" + kind), P=document, NS=ns20, Pg=page)
    )
    e.K = Array([Dictionary(Type=Name.OBJR, Obj=k) for k in kids])
    return e


paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)

# Test 1: a field widget whose enclosing element is a paragraph, not a Form.
misplaced = widget("misplaced", 700, struct_parent=1)
wrapper_p = element("P", [misplaced])

# Test 2: one Form element enclosing two widgets.
paired_a = widget("paired1", 660, struct_parent=2)
paired_b = widget("paired2", 620, struct_parent=3)
form_two = element("Form", [paired_a, paired_b])

# Control: a Form enclosing exactly one widget.
single = widget("single", 580, struct_parent=4)
form_one = element("Form", [single])

# Control: a widget outside the structure tree entirely, which test 1 permits
# because its structure parent type is null.
unparented = widget("unparented", 540)

document.K = Array([paragraph, wrapper_p, form_two, form_one])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0, Array([paragraph]),
                1, wrapper_p,
                2, form_two,
                3, form_two,
                4, form_one,
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 5

page.Annots = Array([misplaced, paired_a, paired_b, single, unparented])

# Test 3: XFA is forbidden outright.
pdf.Root.AcroForm = Dictionary(
    Fields=Array([misplaced, paired_a, paired_b, single, unparented]),
    XFA=pdf.make_indirect(
        Stream(pdf, b"<xdp:xdp xmlns:xdp='http://ns.adobe.com/xdp/'></xdp:xdp>")
    ),
)

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /Metadata.
save(pdf, "formstruct_ua2.pdf", min_version="2.0")
