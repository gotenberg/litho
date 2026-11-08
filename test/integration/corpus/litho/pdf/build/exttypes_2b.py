#!/usr/bin/env python3
"""Build exttypes_2b.pdf: the value types an extension schema entry resolves.

veraPDF 1.30.2 builds one validators container per pdfaSchema entry: the
part's predefined types plus the types that entry defines, where a type with
fields and no pdfaType:namespaceURI registers nothing. An entry counts only
with a namespace URI and a property array, the last one per URI replacing the
others. A pdfaProperty:valueType or pdfaField:valueType is judged defined
against the container of the entry that counts for its entry's URI, and
against the basic types alone when none counts. A property of a namespace no
entry declares, one only a value type names, is undeclared. litho resolved
every type against the types of every entry, and the predefined ones against
any entry.

Four entries, each construct measured on veraPDF 1.30.2 with a crafted file:

| Construct                                                       | Rule           |
| --------------------------------------------------------------- | -------------- |
| entry 1 declares `ex:old` of type `Pair`, which it defines;     | `6.6.2.3.3/8`  |
| entry 2, same URI, replaces it and defines nothing              |                |
| entry 3 has no property array; its type `Box` has fields of     | `6.6.2.3.3/17` |
| type Dimensions, Locale and Box, and one of type Text           | (three times)  |
| `et:a` written at top level, `et` naming only value types       | `6.6.2.3.1/1`, |
|                                                                 | `6.6.2.3.1/2`  |

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

EX = "http://example.com/exttypes/ex/"
EY = "http://example.com/exttypes/ey/"
ET = "http://example.com/exttypes/et/"

pdf = pikepdf.new()

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Contents=Stream(pdf, b"0 0 m\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))


def prop(name, value_type):
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaProperty:name>%s</pdfaProperty:name>"
        "<pdfaProperty:valueType>%s</pdfaProperty:valueType>"
        "<pdfaProperty:category>internal</pdfaProperty:category>"
        "<pdfaProperty:description>%s</pdfaProperty:description>"
        "</rdf:li>" % (name, value_type, name)
    )


def field(name, value_type):
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaField:name>%s</pdfaField:name>"
        "<pdfaField:valueType>%s</pdfaField:valueType>"
        "<pdfaField:description>%s</pdfaField:description>"
        "</rdf:li>" % (name, value_type, name)
    )


def value_type(name, fields):
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaType:type>%s</pdfaType:type>"
        "<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>"
        "<pdfaType:prefix>et</pdfaType:prefix>"
        "<pdfaType:description>%s</pdfaType:description>"
        "<pdfaType:field><rdf:Seq>%s</rdf:Seq></pdfaType:field>"
        "</rdf:li>" % (name, ET, name, "".join(field(*f) for f in fields))
    )


def entry(uri, prefix, props=None, types=None):
    body = (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaSchema:schema>exttypes</pdfaSchema:schema>"
        "<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"
        "<pdfaSchema:prefix>%s</pdfaSchema:prefix>" % (uri, prefix)
    )
    if props is not None:
        body += "<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>" % "".join(
            prop(*p) for p in props
        )
    if types is not None:
        body += "<pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>" % "".join(types)
    return body + "</rdf:li>"


ENTRIES = (
    entry(EX, "ex", [("old", "Pair")], [value_type("Pair", [("a", "Text")])])
    + entry(EX, "ex", [("kept", "Text")])
    + entry(
        EY,
        "ey",
        None,
        [value_type("Box", [("d", "Dimensions"), ("l", "Locale"), ("s", "Box"), ("t", "Text")])],
    )
)

XMP = """<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"
    xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"
    xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"
    xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"
    xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#">
   <pdfaid:part>2</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <pdfaExtension:schemas><rdf:Bag>%s</rdf:Bag></pdfaExtension:schemas>
  </rdf:Description>
  <rdf:Description rdf:about=""
    xmlns:ex="%s"
    xmlns:et="%s">
   <ex:kept>k</ex:kept>
   <et:a>x</et:a>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>""" % (
    ENTRIES,
    EX,
    ET,
)

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "exttypes_2b.pdf", preserve_pdfa=False, fix_metadata_version=False)
