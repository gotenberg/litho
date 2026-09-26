#!/usr/bin/env python3
"""Build extvalue_2b.pdf: extension properties against their declared types.

ISO 19005-2, 6.6.2.3.1 requires every XMP property to use a predefined schema
or an extension schema that complies with 6.6.2.3.2, and veraPDF reads that as
test 2: the property's value shall be of the type its schema declares. For an
extension schema it validates the value the way its ValidatorsContainer does:
an array type the array form, item by item; a simple type a simple value, the
Boolean, Integer and Date types their text; and a type the entry defines with
fields a structure whose every field is one of them, in the type's namespace.
A type the entry defines with no field is Text. litho checked only that a
declared type resolves, never the value against it.

One schema entry declares eight properties of the namespace
http://example.com/extvalue/ (prefix ex), each measured on veraPDF 1.30.2 with a
crafted PDF/A-2b file:

| Property     | Declared type  | Value                                  | Rule          |
| ------------ | -------------- | -------------------------------------- | ------------- |
| `ex:fonts`   | `bag Text`     | a bag of rdf:Description structures    | `6.6.2.3.1/2` |
| `ex:count`   | `Integer`      | `abc`                                  | `6.6.2.3.1/2` |
| `ex:flag`    | `Boolean`      | `yes`                                  | `6.6.2.3.1/2` |
| `ex:list`    | `seq Text`     | an rdf:Bag                             | `6.6.2.3.1/2` |
| `ex:langs`   | `Lang Alt`     | an rdf:Alt whose item has no xml:lang  | `6.6.2.3.1/2` |
| `ex:alias`   | `Alias`, no field | text                                | silent        |
| `ex:good`    | `bag Pair`     | a bag of Pair structures               | silent        |
| `ex:info`    | `Pair`         | an rdf:Description whose fields are ex | silent        |

The three controls are each a shape litho reported that veraPDF passes, or
would once the value is read: a type with no field is Text, and the fields of
a structure written as a nested rdf:Description are no properties of their own,
even in the property's namespace.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

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


PROPS = "".join(
    prop(n, t)
    for n, t in [
        ("fonts", "bag Text"),
        ("count", "Integer"),
        ("flag", "Boolean"),
        ("list", "seq Text"),
        ("langs", "Lang Alt"),
        ("alias", "Alias"),
        ("good", "bag Pair"),
        ("info", "Pair"),
    ]
)

TYPES = (
    '<rdf:li rdf:parseType="Resource">'
    "<pdfaType:type>Pair</pdfaType:type>"
    "<pdfaType:namespaceURI>http://example.com/extvalue/</pdfaType:namespaceURI>"
    "<pdfaType:prefix>ex</pdfaType:prefix>"
    "<pdfaType:description>a pair</pdfaType:description>"
    "<pdfaType:field><rdf:Seq>"
    '<rdf:li rdf:parseType="Resource"><pdfaField:name>a</pdfaField:name>'
    "<pdfaField:valueType>Text</pdfaField:valueType>"
    "<pdfaField:description>a</pdfaField:description></rdf:li>"
    '<rdf:li rdf:parseType="Resource"><pdfaField:name>b</pdfaField:name>'
    "<pdfaField:valueType>Integer</pdfaField:valueType>"
    "<pdfaField:description>b</pdfaField:description></rdf:li>"
    "</rdf:Seq></pdfaType:field></rdf:li>"
    '<rdf:li rdf:parseType="Resource">'
    "<pdfaType:type>Alias</pdfaType:type>"
    "<pdfaType:namespaceURI>http://example.com/extvalue/</pdfaType:namespaceURI>"
    "<pdfaType:prefix>ex</pdfaType:prefix>"
    "<pdfaType:description>an alias of Text</pdfaType:description>"
    "</rdf:li>"
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
   <pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType="Resource">
    <pdfaSchema:schema>extvalue</pdfaSchema:schema>
    <pdfaSchema:namespaceURI>http://example.com/extvalue/</pdfaSchema:namespaceURI>
    <pdfaSchema:prefix>ex</pdfaSchema:prefix>
    <pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>
    <pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>
   </rdf:li></rdf:Bag></pdfaExtension:schemas>
  </rdf:Description>
  <rdf:Description rdf:about=""
    xmlns:ex="http://example.com/extvalue/"
    xmlns:exs="http://example.com/extvalue/font#">
   <ex:fonts><rdf:Bag><rdf:li><rdf:Description>
    <exs:fontName>Helvetica</exs:fontName>
   </rdf:Description></rdf:li></rdf:Bag></ex:fonts>
   <ex:count>abc</ex:count>
   <ex:flag>yes</ex:flag>
   <ex:list><rdf:Bag><rdf:li>x</rdf:li></rdf:Bag></ex:list>
   <ex:langs><rdf:Alt><rdf:li>x</rdf:li></rdf:Alt></ex:langs>
   <ex:alias>text</ex:alias>
   <ex:good><rdf:Bag><rdf:li rdf:parseType="Resource">
    <ex:a>one</ex:a><ex:b>1</ex:b>
   </rdf:li></rdf:Bag></ex:good>
   <ex:info><rdf:Description><ex:a>two</ex:a><ex:b>2</ex:b></rdf:Description></ex:info>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>""" % (
    PROPS,
    TYPES,
)

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "extvalue_2b.pdf", preserve_pdfa=False, fix_metadata_version=False)
