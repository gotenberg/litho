#!/usr/bin/env python3
"""Build afref_3b.pdf: file specifications something refers to but no /AF lists.

ISO 19005-3, 6.8 test 4 asks every file specification dictionary of a PDF/A-3
file to be an associated file, and only an /AF array associates one (ISO
32000-2, 14.13). A reference from elsewhere names the file without stating its
relationship to the document, whether the file is embedded or external and
whether the dictionary is indirect or written inline. veraPDF reads it that
way: isAssociatedFile holds only the objects /AF arrays list. Every file below
is external, so no other 6.8 test applies to it, and each carries /F, /UF and
/AFRelationship:

| Annotation | Route to the file specification                    | 6.8/4  |
| ---------- | -------------------------------------------------- | ------ |
| `FA1`      | a FileAttachment `/FS` naming an indirect filespec | fires  |
| `FA2`      | a FileAttachment `/FS` written inline              | fires  |
| `LK`       | a Link's `/GoToR` action whose `/F` is inline      | fires  |
| `RM`       | a RichMedia `/Assets` tree naming an indirect one  | fires  |
| `FA3`      | a FileAttachment `/FS` the catalog `/AF` also lists | silent |

Four specifications answer, so a check that accepts one route as an
association, or stops at the first defect, reports a count other than four.
`FA3` is the conforming case of the very route `FA1` breaks.

The catalog also carries a `/Names /JavaScript` tree written as an empty
dictionary. ISO 32000-1, 7.7.4 makes the tree a map of document-level
JavaScript actions, and an empty one holds none, so no action-type rule
(ISO 19005-1, 6.6.1 and ISO 19005-2/3, 6.5.1) may fire on it.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

RECT = Array([10, 10, 30, 30])


def external(name):
    """An external file specification, conforming in every key 6.8 reads."""
    return Dictionary(
        Type=Name.Filespec,
        F=String(name),
        UF=String(name),
        AFRelationship=Name("/Unspecified"),
    )


def appearance():
    """An empty normal appearance the size of the annotation."""
    ap = Stream(pdf, b"")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 20, 20])
    return Dictionary(N=pdf.make_indirect(ap))


def annot(subtype, **entries):
    """A printable annotation with an appearance, carrying entries."""
    d = Dictionary(Type=Name.Annot, Subtype=Name(subtype), Rect=RECT, F=4, AP=appearance())
    for key, value in entries.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


listed = pdf.make_indirect(external("e.pdf"))
annots = Array(
    [
        annot("/FileAttachment", FS=pdf.make_indirect(external("a.pdf"))),
        annot("/FileAttachment", FS=external("b.pdf")),
        annot("/Link", A=Dictionary(S=Name.GoToR, F=external("c.pdf"), D=Array([0, Name.Fit]))),
        annot(
            "/RichMedia",
            RichMediaContent=pdf.make_indirect(
                Dictionary(
                    Type=Name("/RichMediaContent"),
                    Assets=Dictionary(Names=Array([String("d.prc"), pdf.make_indirect(external("d.prc"))])),
                )
            ),
        ),
        annot("/FileAttachment", FS=listed),
    ]
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(),
        Annots=annots,
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AF = Array([listed])
pdf.Root.Names = Dictionary(JavaScript=pdf.make_indirect(Dictionary()))
attach_xmp(pdf, xmp_packet(part=3, conformance="B"))

# Deliberately no output intent: nothing on the page paints colour.
save(pdf, "afref_3b.pdf", min_version="1.7")
