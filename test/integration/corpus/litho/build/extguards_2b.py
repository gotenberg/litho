#!/usr/bin/env python3
"""Build extguards_2b.pdf: extension-declared values veraPDF 1.30.2 accepts.

Every property below passes ISO 19005-2, 6.6.2.3.1 on veraPDF 1.30.2, and
each is a shape litho reported at some point, so a firing of that clause at
PDF/A-2b is a false positive. The one rule this fixture fires in 6.6.2.3 is
6.6.2.3.3 test 8, for the unknown type `Nope` declared for xmp:CreateDate.

| Property          | Declaration                          | Value                            | Why it passes                                |
| ----------------- | ------------------------------------ | -------------------------------- | -------------------------------------------- |
| `ex:gps`          | `GPSCoordinate`                      | `10,20.5N`                       | a type of the part, its pattern matched      |
| `ex:mark`         | `Marker`                             | `xmpDM:startTime` a structure    | the field is of type any                     |
| `ex:dim`          | `Dimensions`                         | `stDim:w`, `h` and `unit`        | every field of the type, of its type         |
| `ex:thumb`        | `Thumbnail`                          | `xmpGImg:format` holding `PNG`   | a closed choice is read as Text              |
| `ex:col`          | `Colorant`                           | `xmpG:mode` holding `XYZ`        | likewise                                     |
| `ex:xp1`/`2`/`3`  | `XPath`                              | `a:b`, `$x`, `x:f(1)/a`          | compiles with the JDK's XPath compiler       |
| `xmp:CreateDate`  | `Nope`, which nothing defines        | a date                           | undeclared, so judged as the predefined Date |
| `xmpMM:DerivedFrom` | `RefX`, holding `stRef:filePath`   | a RefX structure                 | judged against its declaration first         |
| `dc:title`        | none; dc extended with `dc:foo`      | a Lang Alt                       | judged against its predefined type           |

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

EX = "http://example.com/extguards/ex/"
REF = "http://ns.adobe.com/xap/1.0/sType/ResourceRef#"

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


def entry(uri, prefix, props, types=""):
    valuetypes = ""
    if types:
        valuetypes = "<pdfaSchema:valueType><rdf:Seq>%s</rdf:Seq></pdfaSchema:valueType>" % types
    return (
        '<rdf:li rdf:parseType="Resource">'
        "<pdfaSchema:schema>extguards</pdfaSchema:schema>"
        "<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>"
        "<pdfaSchema:prefix>%s</pdfaSchema:prefix>"
        "<pdfaSchema:property><rdf:Seq>%s</rdf:Seq></pdfaSchema:property>"
        "%s</rdf:li>" % (uri, prefix, "".join(prop(*p) for p in props), valuetypes)
    )


REFX = (
    '<rdf:li rdf:parseType="Resource">'
    "<pdfaType:type>RefX</pdfaType:type>"
    "<pdfaType:namespaceURI>%s</pdfaType:namespaceURI>"
    "<pdfaType:prefix>stRef</pdfaType:prefix>"
    "<pdfaType:description>a reference holding a file path</pdfaType:description>"
    "<pdfaType:field><rdf:Seq>"
    '<rdf:li rdf:parseType="Resource"><pdfaField:name>filePath</pdfaField:name>'
    "<pdfaField:valueType>Text</pdfaField:valueType>"
    "<pdfaField:description>path</pdfaField:description></rdf:li>"
    "</rdf:Seq></pdfaType:field></rdf:li>" % REF
)

ENTRIES = (
    entry(
        EX,
        "ex",
        [
            ("gps", "GPSCoordinate"),
            ("mark", "Marker"),
            ("dim", "Dimensions"),
            ("thumb", "Thumbnail"),
            ("col", "Colorant"),
            ("xp1", "XPath"),
            ("xp2", "XPath"),
            ("xp3", "XPath"),
        ],
    )
    + entry("http://ns.adobe.com/xap/1.0/", "xmp", [("CreateDate", "Nope")])
    + entry("http://ns.adobe.com/xap/1.0/mm/", "xmpMM", [("DerivedFrom", "RefX")], REFX)
    + entry("http://purl.org/dc/elements/1.1/", "dc", [("foo", "Text")])
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
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
    xmlns:ex="%s"
    xmlns:stDim="http://ns.adobe.com/xap/1.0/sType/Dimensions#"
    xmlns:stRef="%s"
    xmlns:xmpG="http://ns.adobe.com/xap/1.0/g/"
    xmlns:xmpGImg="http://ns.adobe.com/xap/1.0/g/img/"
    xmlns:xmpDM="http://ns.adobe.com/xmp/1.0/DynamicMedia/">
   <ex:gps>10,20.5N</ex:gps>
   <ex:mark rdf:parseType="Resource"><xmpDM:startTime rdf:parseType="Resource"><xmpDM:frames>1</xmpDM:frames></xmpDM:startTime></ex:mark>
   <ex:dim rdf:parseType="Resource"><stDim:w>1.5</stDim:w><stDim:h>2</stDim:h><stDim:unit>inch</stDim:unit></ex:dim>
   <ex:thumb rdf:parseType="Resource"><xmpGImg:format>PNG</xmpGImg:format></ex:thumb>
   <ex:col rdf:parseType="Resource"><xmpG:mode>XYZ</xmpG:mode></ex:col>
   <ex:xp1>a:b</ex:xp1>
   <ex:xp2>$x</ex:xp2>
   <ex:xp3>x:f(1)/a</ex:xp3>
   <xmp:CreateDate>2020-01-01</xmp:CreateDate>
   <xmpMM:DerivedFrom rdf:parseType="Resource"><stRef:filePath>a.pdf</stRef:filePath></xmpMM:DerivedFrom>
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">extguards</rdf:li></rdf:Alt></dc:title>
   <dc:foo>f</dc:foo>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>""" % (
    ENTRIES,
    EX,
    REF,
)

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "extguards_2b.pdf", preserve_pdfa=False, fix_metadata_version=False)
