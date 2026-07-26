#!/usr/bin/env python3
"""Build extschema_2b.pdf: XMP extension schema structure defects.

The coverage scan showed ISO 19005-2, 6.6.2.3.3 as the largest unreached
cluster in the corpus at 18 rules. It describes the shape an extension schema
must have: which fields are required on a schema, a property and a value
type, and which namespace prefix each must carry. All of it is XMP text, so
it costs nothing to plant.

Three schema entries sit in the same bag. The first is defective in its own
fields, the second has defective properties, and the third conforms
completely. Sibling entries are the point: a check that reports the first bad
schema and stops never reaches the second, and a check that condemns the bag
once a defect is seen wrongly reports the third.

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

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (extension schemas) Tj ET\n"),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


def prop(name, value_type, category, description, name_prefix="pdfaProperty"):
    """One pdfaProperty rdf:li. Any argument may be None to omit that field."""
    parts = []
    if name is not None:
        parts.append(f"<{name_prefix}:name>{name}</{name_prefix}:name>")
    if value_type is not None:
        parts.append(f"<pdfaProperty:valueType>{value_type}</pdfaProperty:valueType>")
    if category is not None:
        parts.append(f"<pdfaProperty:category>{category}</pdfaProperty:category>")
    if description is not None:
        parts.append(
            f"<pdfaProperty:description>{description}</pdfaProperty:description>"
        )
    return '<rdf:li rdf:parseType="Resource">' + "".join(parts) + "</rdf:li>"


def schema(name, uri, prefix, props):
    parts = []
    if name is not None:
        parts.append(f"<pdfaSchema:schema>{name}</pdfaSchema:schema>")
    if uri is not None:
        parts.append(f"<pdfaSchema:namespaceURI>{uri}</pdfaSchema:namespaceURI>")
    if prefix is not None:
        parts.append(f"<pdfaSchema:prefix>{prefix}</pdfaSchema:prefix>")
    parts.append(
        "<pdfaSchema:property><rdf:Seq>" + "".join(props) + "</rdf:Seq></pdfaSchema:property>"
    )
    return '<rdf:li rdf:parseType="Resource">' + "".join(parts) + "</rdf:li>"


# Schema one omits pdfaSchema:prefix (6.6.2.3.3 t4). Its single property is
# well formed, so the defect is confined to the schema's own fields.
s1 = schema(
    "Litho Alpha Schema",
    "http://example.org/litho/alpha#",
    None,
    [prop("alphaOne", "Text", "external", "first alpha property")],
)

# Schema two is well formed itself; its properties carry the defects. One has
# a category outside {external, internal} (t9), one omits its description
# (t10), and one omits its name (t7). Three sibling properties, three
# independent rules.
s2 = schema(
    "Litho Beta Schema",
    "http://example.org/litho/beta#",
    "lbeta",
    [
        prop("betaOne", "Text", "sideways", "category is not external or internal"),
        prop("betaTwo", "Text", "external", None),
        prop(None, "Text", "external", "this property has no name"),
    ],
)

# Schema three conforms in every respect. A firing against it would be a
# false positive, and its silence shows the walk did not condemn the whole
# bag once it saw a defect in an earlier entry.
s3 = schema(
    "Litho Gamma Schema",
    "http://example.org/litho/gamma#",
    "lgamma",
    [prop("gammaOne", "Text", "internal", "fully conforming property")],
)

packet = (
    '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>'
    '<x:xmpmeta xmlns:x="adobe:ns:meta/">'
    '<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
    '<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">'
    "<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>"
    "</rdf:Description>"
    '<rdf:Description rdf:about=""'
    ' xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"'
    ' xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"'
    ' xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">'
    "<pdfaExtension:schemas><rdf:Bag>" + s1 + s2 + s3 + "</rdf:Bag></pdfaExtension:schemas>"
    "</rdf:Description>"
    "</rdf:RDF></x:xmpmeta><?xpacket end=\"w\"?>"
).encode("utf-8")

meta = Stream(pdf, packet)
meta.Type = Name.Metadata
meta.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(meta)

# Deliberately no /OutputIntents, so the colour rules stay reachable too.
save(pdf, "extschema_2b.pdf", min_version="1.7")
