#!/usr/bin/env python3
"""Build xmppage_2b.pdf: the extension-schema tests on the XMP packets pages carry.

ISO 19005-2, 6.6.2.3 constrains the properties and the extension schemas of
every metadata stream in the file, not only the catalog's. veraPDF 1.30.2
models each page's packet as an XMPPackage whose properties resolve through
its own extension schemas and the catalog packet's, and whose schemas answer
the structure tests as the catalog packet's do. Each page's packet plants one
kind of defect, and each rule of 6.6.2.3 the file fires comes from one page:

| Page | Packet                                                          | Rule at PDF/A-2b                        |
| ---- | --------------------------------------------------------------- | --------------------------------------- |
| 1    | `eu:foo`, a namespace no packet declares                        | `6.6.2.3.1/1` and `/2`                  |
| 1    | `ex:pt`, declared by the catalog packet alone                   | silent                                  |
| 2    | a property with no description, one of category `sideways`      | `6.6.2.3.3/10`, `6.6.2.3.3/9`           |
| 2    | an entry with no prefix, a value type with no description       | `6.6.2.3.3/4`, `6.6.2.3.3/14`           |
| 2    | a `dc:stray` field inside a schema entry                        | `6.6.2.3.2/1`                           |
| 3    | `pdfaExtension:schemas` written as an `rdf:Seq`                 | `6.6.2.3.3/1`                           |
| 4    | `ex:pa` of type `MainT`, which the catalog's `ex` entry defines | silent                                  |
| 4    | an entry with no property array, a field of type `Dimensions`   | `6.6.2.3.3/17`                          |

Page 4's entry for `ex` resolves `MainT` through the catalog packet's entry
for the same namespace, as veraPDF builds a page entry's validators on a copy
of the catalog entry's, so neither `6.6.2.3.3/8` nor `6.6.2.3.1` fires there.
Its entry with no property array registers nothing, so its field types are
judged against the basic types alone, which hold no `Dimensions`.

No property name repeats across packets: veraPDF checks one property per
namespace, prefix and name in a document, the first its traversal meets, and a
repeated one would make the verdict depend on that order.

ISO 19005-1 builds no package for a page's metadata stream, so the page
packets are silent at PDF/A-1b.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

EX = "http://example.com/xmppage/ex/"
MT = "http://example.com/xmppage/mt/"

NS = (
    ' xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"'
    ' xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"'
    ' xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"'
    ' xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"'
    ' xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"'
    ' xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"'
    ' xmlns:dc="http://purl.org/dc/elements/1.1/"'
    ' xmlns:xmp="http://ns.adobe.com/xap/1.0/"'
    ' xmlns:ex="%s" xmlns:mt="%s"'
    ' xmlns:pa="http://example.com/xmppage/pa/"'
    ' xmlns:pd="http://example.com/xmppage/pd/"'
    ' xmlns:eu="http://example.com/xmppage/eu/"' % (EX, MT)
)


def prop(name, value_type, category="internal", description=True):
    desc = "<pdfaProperty:description>%s</pdfaProperty:description>" % name if description else ""
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaProperty:name>%s</pdfaProperty:name>"
        "<pdfaProperty:valueType>%s</pdfaProperty:valueType>"
        "<pdfaProperty:category>%s</pdfaProperty:category>%s"
        "</rdf:li>" % (name, value_type, category, desc)
    )


def value_type(name, uri, prefix, fields, description=True):
    desc = "<pdfaType:description>%s</pdfaType:description>" % name if description else ""
    items = "".join(
        '<rdf:li rdf:parseType="Resource"><pdfaField:name>%s</pdfaField:name>'
        "<pdfaField:valueType>%s</pdfaField:valueType>"
        "<pdfaField:description>%s</pdfaField:description></rdf:li>" % (f, t, f)
        for f, t in fields
    )
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaType:type>%s</pdfaType:type>"
        "<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>"
        "<pdfaType:prefix>%s</pdfaType:prefix>%s"
        "<pdfaType:field><rdf:Seq>%s</rdf:Seq></pdfaType:field>"
        "</rdf:li>" % (name, uri, prefix, desc, items)
    )


def entry(uri, prefix, props=None, types="", extra=""):
    pfx = "<pdfaSchema:prefix>%s</pdfaSchema:prefix>" % prefix if prefix else ""
    properties = ""
    if props is not None:
        properties = "<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>" % "".join(props)
    valuetypes = ""
    if types:
        valuetypes = "<pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>" % types
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaSchema:schema>xmppage</pdfaSchema:schema>"
        "<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"
        "%s%s%s%s</rdf:li>" % (uri, pfx, properties, valuetypes, extra)
    )


def packet(head="", schemas="", container="Bag", body=""):
    ext = ""
    if schemas:
        ext = "<pdfaExtension:schemas><rdf:%s>%s</rdf:%s></pdfaExtension:schemas>" % (container, schemas, container)
    return (
        '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
        '<x:xmpmeta xmlns:x="adobe:ns:meta/">\n'
        ' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
        '  <rdf:Description rdf:about=""%s>\n'
        "   %s%s\n"
        "  </rdf:Description>\n"
        '  <rdf:Description rdf:about=""%s>\n'
        "   %s\n"
        "  </rdf:Description>\n"
        " </rdf:RDF>\n"
        "</x:xmpmeta>\n"
        '<?xpacket end="w"?>' % (NS, head, ext, NS, body)
    ).encode("utf-8")


def metadata(pdf, data):
    meta = Stream(pdf, data)
    meta.Type = Name.Metadata
    meta.Subtype = Name.XML
    return pdf.make_indirect(meta)


CATALOG = packet(
    head="<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>",
    schemas=entry(EX, "ex", [prop("pt", "Text"), prop("pm", "MainT")], value_type("MainT", MT, "mt", [("a", "Text")])),
    body='<dc:title><rdf:Alt><rdf:li xml:lang="x-default">xmppage</rdf:li></rdf:Alt></dc:title>',
)

PAGES = [
    packet(body="<eu:foo>bar</eu:foo><ex:pt>declared by the catalog packet</ex:pt><xmp:CreatorTool>litho</xmp:CreatorTool>"),
    packet(
        schemas=entry(
            "http://example.com/xmppage/pa/",
            "pa",
            [prop("p1", "Text", description=False), prop("p2", "Text", category="sideways")],
            extra="<dc:stray>x</dc:stray>",
        )
        + entry("http://example.com/xmppage/pb/", "", [prop("q", "Text")])
        + entry(
            "http://example.com/xmppage/pc/",
            "pc",
            [prop("r", "T")],
            value_type("T", "http://example.com/xmppage/pct/", "pct", [("a", "Text")], description=False),
        ),
        body="<pa:p1>x</pa:p1>",
    ),
    packet(
        schemas=entry("http://example.com/xmppage/pd/", "pd", [prop("s", "Text")]),
        container="Seq",
        body="<pd:s>x</pd:s>",
    ),
    packet(
        schemas=entry(EX, "ex", [prop("pa", "MainT")])
        + entry(
            "http://example.com/xmppage/pe/",
            "pe",
            None,
            value_type("TW", "http://example.com/xmppage/tw/", "tw", [("d", "Dimensions")]),
        ),
        body='<ex:pa rdf:parseType="Resource"><mt:a>x</mt:a></ex:pa>',
    ),
]

pdf = pikepdf.new()
for data in PAGES:
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(),
            Contents=Stream(pdf, b"0 0 m\n"),
            Metadata=metadata(pdf, data),
        )
    )
    pdf.pages.append(pikepdf.Page(page))
pdf.Root.Metadata = metadata(pdf, CATALOG)

save(pdf, "xmppage_2b.pdf", preserve_pdfa=False, fix_metadata_version=False)
