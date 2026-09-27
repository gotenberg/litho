#!/usr/bin/env python3
"""Build fieldkids_2b.pdf: form fields whose /Kids arrays are objects of their
own, at PDF/A-2b.

ISO 32000-1, 12.7.3.1, Table 220 makes a field's /Kids an array of indirect
references; nothing stops the array itself being written as an object of its
own and named by reference. Every walk over the field tree reads such an array
once, because the direct fields it holds could each name it again. No corpus
file names a field's /Kids by reference, so this fixture gives the gate the
shape:

| Field       | Kind          | /Kids                   | Widgets                         |
| ----------- | ------------- | ----------------------- | ------------------------------- |
| group       | non-terminal  | an array, by reference  | none of its own                 |
| group.first | text, merged  | none                    | itself, with an appearance      |
| group.inner | text          | an array, by reference  | one, whose /AA holds JavaScript |
| group.note  | text, direct  | none                    | none: written in group's /Kids  |
| check       | check box     | an array, by reference  | one, with no appearance         |

The page shows nothing. Every appearance paints nothing. The widget of
group.inner fails ISO 19005-2, 6.4.1 test 1 (/AA on a widget), and the widget
of check fails 6.3.3 test 1 (no /AP); at part 1 they fail ISO 19005-1, 6.6.2
test 1 and 6.9 test 2. The XMP packet claims PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def appearance():
    """A 20 by 20 Form XObject appearance that paints nothing."""
    s = Stream(pdf, b"0 0 20 20 re n\n")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(s)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"")),
    )
)
pdf.Root.Pages.Kids = Array([page])
pdf.Root.Pages.Count = 1

group = pdf.make_indirect(Dictionary(T=String("group")))
first = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Parent=group,
        T=String("first"),
        FT=Name.Tx,
        V=String("one"),
        Rect=Array([72, 700, 92, 720]),
        P=page,
        F=4,
        AP=Dictionary(N=appearance()),
    )
)
inner = pdf.make_indirect(Dictionary(Parent=group, T=String("inner"), FT=Name.Tx, V=String("two")))
inner_widget = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Parent=inner,
        Rect=Array([72, 650, 92, 670]),
        P=page,
        F=4,
        AP=Dictionary(N=appearance()),
        AA=Dictionary(K=Dictionary(S=Name.JavaScript, JS=String("1;"))),
    )
)
inner.Kids = pdf.make_indirect(Array([inner_widget]))
note = Dictionary(T=String("note"), FT=Name.Tx, V=String("three"))
group.Kids = pdf.make_indirect(Array([first, inner, note]))

check = pdf.make_indirect(Dictionary(T=String("check"), FT=Name.Btn, V=Name.Off))
check_widget = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Parent=check,
        Rect=Array([72, 600, 92, 620]),
        P=page,
        F=4,
        AS=Name.Off,
    )
)
check.Kids = pdf.make_indirect(Array([check_widget]))

page.Annots = Array([first, inner_widget, check_widget])
pdf.Root.AcroForm = Dictionary(Fields=Array([group, check]))
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "fieldkids_2b.pdf")
