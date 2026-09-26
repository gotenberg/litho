#!/usr/bin/env python3
"""Build mcform_2a.pdf: marked-content /Lang in forms and annotation appearances.

ISO 19005-2, 6.7.4 holds every /Lang entry, "in the document catalog
dictionary or in a structure element dictionary or property list", to the
language tag grammar. veraPDF 1.30.2 judges the property list of every BDC
operator its content walk reaches, and the walk enters the Form XObjects a
page draws and the appearance streams of its annotations; each shape below
was measured with a crafted file at PDF/A-1a and PDF/A-2a. litho read the
page's own content alone, and reported none of them:

| Where                                                       | /Lang | Rule      |
| ----------------------------------------------------------- | ----- | --------- |
| inline BDC in a Form XObject the page draws twice           | `5v`  | `6.7.4/1` |
| inline BDC in an annotation's normal appearance             | `6u`  | `6.7.4/1` |
| BDC in a form with no /Resources, naming the page's /P0     | `2y`  | `6.7.4/1` |

The form drawn twice counts once. The form with no /Resources resolves /P0
through the page's resources, as veraPDF does.

The file is tagged, with a structure tree of one /Document element that owns
no content. Its sibling mclang_2a.pdf carries the shapes veraPDF does not
judge and litho reported.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def form(body, resources=True):
    f = Stream(pdf, body)
    f.Type = Name.XObject
    f.Subtype = Name.Form
    f.BBox = Array([0, 0, 10, 10])
    if resources:
        f.Resources = Dictionary()
    return pdf.make_indirect(f)


annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([100, 100, 110, 110]),
        F=4,
        AP=Dictionary(N=form(b"/Span <</Lang (6u)>> BDC EMC\n")),
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Properties=Dictionary(P0=pdf.make_indirect(Dictionary(Lang=String("2y")))),
            XObject=Dictionary(
                Fm0=form(b"/Span <</Lang (5v)>> BDC EMC\n"),
                Fm1=form(b"/Span /P0 BDC EMC\n", resources=False),
            ),
        ),
        Contents=Stream(pdf, b"/Artifact BMC /Fm0 Do /Fm0 Do /Fm1 Do EMC\n"),
        Annots=Array([annot]),
    )
)
pdf.pages.append(pikepdf.Page(page))

root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
root.K = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=root))
pdf.Root.StructTreeRoot = root
pdf.Root.MarkInfo = Dictionary(Marked=True)

metadata = Stream(pdf, xmp_packet(part=2, conformance="A"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "mcform_2a.pdf", preserve_pdfa=False, fix_metadata_version=False)
