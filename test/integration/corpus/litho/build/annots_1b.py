#!/usr/bin/env python3
"""Build annots_1b.pdf: annotation dictionary constraints under PDF/A-1b.

The coverage scan showed PDF/A-1b as the weakest PDF/A profile in the corpus,
with ISO 19005-1, 6.5.3 and 6.6.2 unreached between them. Both are annotation
clauses and both are cheap: 6.5.3 constrains the annotation dictionary itself
across six tests, and 6.6.2 forbids an additional-actions dictionary in three
places.

Nine defects sit in one file, each on its own object, which is the masking
probe: a check that reports the first offending annotation and stops reports
one rule where six are due, and a check that condemns every annotation once it
sees one fires on the two conforming controls beside them.

The clauses split by object, which decides where each defect has to live.
6.5.3 is scoped to `PDAnnot`, so its six tests are six sibling annotations on
one page. 6.6.2 splits across `PDWidgetAnnot`, `PDFormField` and `PDDocument`,
so its three tests need a widget, a form field and the catalog respectively.

Test 3 of 6.5.3 is reachable only because the file declares no OutputIntent:
the test permits `/C` and `/IC` when the destination profile is RGB, and with
no output intent at all there is no RGB to permit it. The missing OutputIntent
is itself a separate defect the file already carries.

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

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (annotations) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

# ISO 19005-1, 6.5.3 t2 wants /F present with Print set and Hidden, Invisible
# and NoView clear. Bit 3 (value 4) is Print.
PRINT_FLAG = 4


def appearance():
    """A minimal appearance stream, the value /N normally takes."""
    ap = Stream(pdf, b"0 0 0 rg 0 0 20 20 re f\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(ap)


def annot(subtype, y, **extra):
    """One annotation, conforming unless extra says otherwise."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name("/" + subtype),
        Rect=Array([50, y, 70, y + 20]),
        F=PRINT_FLAG,
    )
    for k, v in extra.items():
        d[Name("/" + k)] = v
    return pdf.make_indirect(d)


annots = []

# t1: /CA other than 1.0.
annots.append(annot("Square", 600, CA=0.5, AP=Dictionary(N=appearance())))

# t2: no /F at all, so the Print flag cannot be set.
a_noflag = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([50, 570, 70, 590]),
    AP=Dictionary(N=appearance()),
)
annots.append(pdf.make_indirect(a_noflag))

# t3: a /C array with no OutputIntent to permit it.
annots.append(
    annot("Square", 540, C=Array([1, 0, 0]), AP=Dictionary(N=appearance()))
)

# t4: an appearance dictionary carrying more than the /N key.
annots.append(
    annot(
        "Square",
        510,
        AP=Dictionary(N=appearance(), D=appearance()),
    )
)

# t5: a Widget whose /FT is /Btn must give /N an appearance subdictionary; this
# one gives a stream. The field keys sit on the annotation, which is the merged
# form the clause describes.
annots.append(
    annot("Widget", 480, FT=Name.Btn, T=String("btn1"), AP=Dictionary(N=appearance()))
)

# t6: a non-Widget annotation must give /N a stream; this one gives a
# subdictionary.
annots.append(
    annot(
        "Square",
        450,
        AP=Dictionary(N=Dictionary(Off=appearance())),
    )
)

# 6.6.2 t1: a Widget annotation carrying /AA.
widget_aa = annot(
    "Widget",
    420,
    FT=Name.Tx,
    T=String("txt1"),
    AP=Dictionary(N=appearance()),
    AA=Dictionary(K=Dictionary(S=Name.JavaScript, JS=String("1;"))),
)
annots.append(widget_aa)

# 6.6.2 t2: a form field carrying /AA. It is a parent field rather than a
# merged widget, so the verdict lands on PDFormField rather than on the widget.
field_kid = annot("Widget", 390, T=String("kid"), AP=Dictionary(N=appearance()))
field_aa = pdf.make_indirect(
    Dictionary(
        FT=Name.Tx,
        T=String("parent"),
        Kids=Array([field_kid]),
        AA=Dictionary(K=Dictionary(S=Name.JavaScript, JS=String("1;"))),
    )
)
field_kid.Parent = field_aa
annots.append(field_kid)

# An appearance dictionary carrying /D beside an /N of the wrong type. Both
# /N-type tests open with the same escape, `AP != "N"`, which holds for any
# dictionary carrying more than /N, so this annotation fails t4 alone: neither
# t5 nor t6 judges it. litho applied that escape to t5 only and reported t6
# here, which is why the case is planted rather than left to the two defects
# that produce it separately.
annots.append(
    annot(
        "Square",
        300,
        AP=Dictionary(N=Dictionary(Off=appearance()), D=appearance()),
    )
)

# Two conforming annotations: a plain square and a plain widget. Both carry a
# correct /F and an /N stream, so a firing against either is over-suppression.
annots.append(annot("Square", 360, AP=Dictionary(N=appearance())))
ok_widget = annot(
    "Widget", 330, FT=Name.Tx, T=String("okwidget"), AP=Dictionary(N=appearance())
)
annots.append(ok_widget)

page.Annots = Array(annots)

pdf.Root.AcroForm = Dictionary(Fields=Array([widget_aa, field_aa, ok_widget]))
# 6.6.2 t3: the catalog carrying /AA.
pdf.Root.AA = Dictionary(WC=Dictionary(S=Name.JavaScript, JS=String("1;")))

# Deliberately no /Metadata and no /OutputIntents: the second keeps 6.5.3 t3
# reachable, and both are independent of the annotation clauses.
save(pdf, "annots_1b.pdf", min_version="1.4")
