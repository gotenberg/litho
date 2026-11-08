#!/usr/bin/env python3
"""Build inlinefs_4.pdf: file specifications written inline, outside any /FS.

ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
object, so one can sit wherever a value can. veraPDF judges a /Type /Filespec
dictionary wherever it meets it, as it walks every value of every object, and
names it by the object holding it. litho judged one only inside an annotation
that is an object of its own, and its repairs reached an annotation's /FS
alone: the media type, /UF and /AFRelationship repairs never saw the others,
and the PDF/A-3 move to an object of its own walked annotations only.

Four routes, each carrying a different set of defects, so a check or a repair
that misses one route reports a count other than the one due:

| Route     | Where the specification sits                     | Embeds            | Keys           |
| --------- | ------------------------------------------------ | ----------------- | -------------- |
| `LK1`     | the /AF of a Link that is an object of its own   | text, no /Subtype | /F only, no AR |
| `LK2`     | the /AF of a Link written inline in /Annots      | text, /plain      | /F /UF AR      |
| `Fm`      | the /AF of a form XObject the page lists         | nothing           | /F /UF, no AR  |
| `catalog` | the catalog's /AF                                | nothing           | /F /UF AR      |

AR is /AFRelationship. Neither embedded file is a PDF. At PDF/A-4, ISO
19005-4, 6.9 judges embedded files alone: t1 (media type) and t3 (not a PDF)
fire on `LK1` and `LK2`, t2 (/UF) and t4 (/AFRelationship) on `LK1`. At
PDF/A-3b, ISO 19005-3, 6.8 t1 fires on `LK1` and `LK2`, t2 on `LK1`, t3 on
`LK1` and `Fm`, and t4 on all four: an /AF array associates only the indirect
objects it lists, and veraPDF counts an inline one as associated with nothing,
even one an /AF array lists inline.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

RECT = Array([10, 10, 30, 30])


def embedded(data, subtype=None):
    """An embedded file stream, with /Subtype only when subtype is given."""
    s = Stream(pdf, data)
    s.Type = Name.EmbeddedFile
    if subtype is not None:
        s.Subtype = Name(subtype)
    return pdf.make_indirect(s)


def appearance():
    """An empty normal appearance the size of the annotation."""
    ap = Stream(pdf, b"")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 20, 20])
    return Dictionary(N=pdf.make_indirect(ap))


def link(spec, contents):
    """A printable Link with an appearance whose /AF lists spec inline."""
    return Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=RECT,
        F=4,
        Border=Array([0, 0, 0]),
        Contents=String(contents),
        AP=appearance(),
        AF=Array([spec]),
    )


lk1 = Dictionary(Type=Name.Filespec, F=String("lk1.txt"), EF=Dictionary(F=embedded(b"lk1\n")))
lk2 = Dictionary(
    Type=Name.Filespec,
    F=String("lk2.txt"),
    UF=String("lk2.txt"),
    AFRelationship=Name.Data,
    EF=Dictionary(F=embedded(b"lk2\n", "/plain")),
)

form = Stream(pdf, b"")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 1, 1])
form.AF = Array([Dictionary(Type=Name.Filespec, F=String("fm.txt"), UF=String("fm.txt"))])

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        # The page lists the form without drawing it: veraPDF judges a file
        # specification wherever it sits, and a drawn form would bring the
        # content rules of the PDF/UA levels in with it.
        Resources=Dictionary(XObject=Dictionary(Fm=pdf.make_indirect(form))),
        # LK1 is an object of its own; LK2 is written inline in /Annots.
        Annots=Array([pdf.make_indirect(link(lk1, "lk1")), link(lk2, "lk2")]),
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AF = Array(
    [
        Dictionary(
            Type=Name.Filespec,
            F=String("cat.txt"),
            UF=String("cat.txt"),
            AFRelationship=Name.Supplement,
        )
    ]
)
attach_xmp(pdf, xmp_packet(part=4, rev=2020))

# Deliberately no output intent: nothing on the page paints colour.
save(pdf, "inlinefs_4.pdf", min_version="2.0")
