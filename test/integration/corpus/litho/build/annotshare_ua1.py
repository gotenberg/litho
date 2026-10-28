#!/usr/bin/env python3
"""Build annotshare_ua1.pdf: annotation objects two pages list, identification
declared on its own element, and page boundaries short of four numbers.

veraPDF judges an annotation object once, on the first page listing it, however
many pages list it (its validator skips an object key it has met). litho judged
it once per listing, so every per-annotation rule counted twice. Page 1 lists
four annotation objects, the Square twice, and page 2 lists the Link, the Text
and the Widget again:

  * a Link without /Contents outside the structure tree: 7.18.5 t1 and t2,
    7.18.1 t2, once each;
  * a Text annotation with no flags, /Contents or appearance: 7.18.1 t1 and t2,
    and the part-2 flag and appearance tests, once each;
  * a text field Widget with no /TU and no appearance: 7.18.1 t3, 7.18.4 t1;
  * a Square under an Annot element whose /Alt differs from its /Contents:
    ISO 14289-2, 8.9.4.2 t1 once, though page 1 lists it twice.

The isOutsideCropBox exemption of ISO 14289-1, 7.18 reads the crop box veraPDF
reads (PDPage.getCropBox): page 2 inherits its media box from the page tree and
lists a Link lying beyond it, and page 3 states a crop box of three numbers,
[0 0 300], whose missing corner reads as 0, so the Link it lists lies above a
box of no height. Both Links are exempt. litho read the page dictionary alone
and held both to the rules.

Page 3 also states an art box of [0 0 400 null]. veraPDF reads a missing corner
and one that is no number as 0 (GFCosBBox.getRequiredValue), so the crop and art
boxes both fail ISO 19005-2 and -3, 6.1.13 t11, which litho skipped.

The XMP packet declares every namespace on the property element that uses it,
as pikepdf's metadata API writes it: pdfuaid:part 1, pdfaid:part 2 and
conformance B, dc:title, and a PDF/A extension schema declaring the pdfuaid
namespace, every element of which declares its own namespace too. litho read
pdfuaid:part through a literal "pdfuaid:part>1<", which met only the closing
tag, and reported ISO 14289-1, 5 t2 on a packet claiming part 1.

No text is drawn, so the pages read the same under every mutation.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
)

PDFUAID = "http://www.aiim.org/pdfua/ns/id/"
PDFAID = "http://www.aiim.org/pdfa/ns/id/"
EXT = "http://www.aiim.org/pdfa/ns/extension/"
SCHEMA = "http://www.aiim.org/pdfa/ns/schema#"
PROPERTY = "http://www.aiim.org/pdfa/ns/property#"
DC = "http://purl.org/dc/elements/1.1/"


def el(prefix, uri, local, inner):
    """An element declaring its own namespace, as pikepdf writes one."""
    return '<%s:%s xmlns:%s="%s">%s</%s:%s>' % (prefix, local, prefix, uri, inner, prefix, local)


def extension_schema():
    prop = (
        '<rdf:li rdf:parseType="Resource">'
        + el("pdfaProperty", PROPERTY, "name", "part")
        + el("pdfaProperty", PROPERTY, "valueType", "Integer")
        + el("pdfaProperty", PROPERTY, "category", "internal")
        + el("pdfaProperty", PROPERTY, "description", "The part of ISO 14289 the file conforms to")
        + "</rdf:li>"
    )
    schema = (
        '<rdf:li rdf:parseType="Resource">'
        + el("pdfaSchema", SCHEMA, "schema", "PDF/UA identification schema")
        + el("pdfaSchema", SCHEMA, "namespaceURI", PDFUAID)
        + el("pdfaSchema", SCHEMA, "prefix", "pdfuaid")
        + el("pdfaSchema", SCHEMA, "property", "<rdf:Seq>" + prop + "</rdf:Seq>")
        + "</rdf:li>"
    )
    return el("pdfaExtension", EXT, "schemas", "<rdf:Bag>" + schema + "</rdf:Bag>")


packet = (
    '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
    '<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="pikepdf">\n'
    ' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
    ' <rdf:Description rdf:about="">'
    + el("pdfuaid", PDFUAID, "part", "1")
    + el("pdfaid", PDFAID, "part", "2")
    + el("pdfaid", PDFAID, "conformance", "B")
    + '</rdf:Description><rdf:Description rdf:about="">'
    + el("dc", DC, "title", '<rdf:Alt><rdf:li xml:lang="x-default">litho corpus fixture</rdf:li></rdf:Alt>')
    + '</rdf:Description><rdf:Description rdf:about="">'
    + extension_schema()
    + "</rdf:Description></rdf:RDF>\n"
    "</x:xmpmeta>\n"
    '<?xpacket end="w"?>\n'
).encode("utf-8")

pdf = pikepdf.new()

tree = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=tree))
annot_elem = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Annot, P=document, Alt=String("another description"))
)

link = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([50, 700, 200, 720]),
        F=4,
        Border=Array([0, 0, 0]),
        A=Dictionary(S=Name.URI, URI=String("https://example.com/")),
    )
)
text = pdf.make_indirect(Dictionary(Type=Name.Annot, Subtype=Name.Text, Rect=Array([50, 650, 70, 670])))
widget = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        FT=Name.Tx,
        T=String("name"),
        Rect=Array([50, 600, 200, 620]),
        F=4,
    )
)
square = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([50, 550, 100, 580]),
        F=4,
        Contents=String("a square"),
        StructParent=0,
    )
)
off_page = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([700, 700, 800, 720]),
        F=4,
        Border=Array([0, 0, 0]),
        A=Dictionary(S=Name.URI, URI=String("https://example.com/off")),
    )
)
above_crop = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Link,
        Rect=Array([50, 700, 200, 720]),
        F=4,
        Border=Array([0, 0, 0]),
        A=Dictionary(S=Name.URI, URI=String("https://example.com/above")),
    )
)

annot_elem.K = Dictionary(Type=Name.OBJR, Obj=square)
document.K = Array([annot_elem])
tree.K = document
tree.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, annot_elem])))
tree.ParentTreeNextKey = 1


def page(**entries):
    d = Dictionary(Type=Name.Page, Resources=Dictionary(), Contents=Stream(pdf, b""), Tabs=Name.S)
    for k, v in entries.items():
        d[Name("/" + k)] = v
    return pdf.make_indirect(d)


page1 = page(MediaBox=Array([0, 0, 612, 792]), Annots=Array([link, text, widget, square, square]))
page2 = page(Annots=Array([link, text, widget, off_page]))
page3 = page(
    MediaBox=Array([0, 0, 612, 792]),
    CropBox=Array([0, 0, 300]),
    ArtBox=Array([0, 0, 400, None]),
    Annots=Array([above_crop]),
)

pages = pdf.Root.Pages
pages.MediaBox = Array([0, 0, 612, 792])
for p in (page1, page2, page3):
    p.Parent = pages
pages.Kids = Array([page1, page2, page3])
pages.Count = 3

widget.P = page1
pdf.Root.AcroForm = Dictionary(Fields=Array([widget]))
pdf.Root.StructTreeRoot = tree
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, packet)

save(pdf, "annotshare_ua1.pdf", fix_metadata_version=False, preserve_pdfa=False)
