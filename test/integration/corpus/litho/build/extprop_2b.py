#!/usr/bin/env python3
"""Build extprop_2b.pdf: the container an extension schema's properties live in.

ISO 19005-2, 6.6.2.3.3 test 5 reads

    isPropertyValidSeq == true && (propertyPrefix == null || propertyPrefix == "pdfaSchema")

so it judges the CONTAINER holding a schema's property list rather than any
property inside it. `extschema_2b.pdf` covers the tests that judge the schema's
own fields and its individual properties; this one covers the container, which
those never reach because a well-formed `rdf:Seq` is what carries them.

| Construct                                      | Rule            |
| ---------------------------------------------- | --------------- |
| a `pdfaSchema:property` holding an `rdf:Bag`   | `6.6.2.3.3/5`   |

Three schemas sit side by side and only the first offends. The second holds the
same single property in a proper `rdf:Seq`, and the third holds two properties
in a `rdf:Seq`, because a container check that merely counted entries would pass
a one-entry list and fail a longer one. Both are controls: every property inside
all three schemas is complete and well formed, so nothing in the file can answer
test 5 except the container itself.

The properties are deliberately identical across the three schemas. If a check
condemned the schema for something a property carries, all three would fire and
the file would prove nothing about the container.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

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
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (extension schema property) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))


def prop(name, description):
    """One complete pdfaProperty entry, so no property-level test can fire."""
    return (
        '<rdf:li rdf:parseType="Resource">'
        f"<pdfaProperty:name>{name}</pdfaProperty:name>"
        "<pdfaProperty:valueType>Text</pdfaProperty:valueType>"
        "<pdfaProperty:category>external</pdfaProperty:category>"
        f"<pdfaProperty:description>{description}</pdfaProperty:description>"
        "</rdf:li>"
    )


def schema(name, uri, prefix, props, container="Seq"):
    """One schema whose property list rides in the named RDF container."""
    return (
        '<rdf:li rdf:parseType="Resource">'
        f"<pdfaSchema:schema>{name}</pdfaSchema:schema>"
        f"<pdfaSchema:namespaceURI>{uri}</pdfaSchema:namespaceURI>"
        f"<pdfaSchema:prefix>{prefix}</pdfaSchema:prefix>"
        f"<pdfaSchema:property><rdf:{container}>"
        + "".join(props)
        + f"</rdf:{container}></pdfaSchema:property>"
        "</rdf:li>"
    )


# The defect: an rdf:Bag where the clause requires an rdf:Seq. The property
# inside it is complete, so the container is the only thing left to answer for.
s_bag = schema(
    "Litho Bag Schema",
    "http://example.org/litho/bag#",
    "lbag",
    [prop("bagOne", "a property in the wrong container")],
    container="Bag",
)

# The control: the same single property in the container the clause requires.
s_seq = schema(
    "Litho Seq Schema",
    "http://example.org/litho/seq#",
    "lseq",
    [prop("seqOne", "a property in the right container")],
)

# The second control carries two properties, so a container check that accepted
# a one-entry list and rejected a longer one is visible here rather than hidden.
s_seq_two = schema(
    "Litho Pair Schema",
    "http://example.org/litho/pair#",
    "lpair",
    [
        prop("pairOne", "first property of the pair"),
        prop("pairTwo", "second property of the pair"),
    ],
)

EXTENSION = (
    '   <pdfaExtension:schemas><rdf:Bag>'
    + s_bag
    + s_seq
    + s_seq_two
    + "</rdf:Bag></pdfaExtension:schemas>"
)

attach_xmp(
    pdf,
    xmp_packet(part=2, conformance="B", extra=EXTENSION).replace(
        b'xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"',
        b'xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"\n'
        b'    xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"\n'
        b'    xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"\n'
        b'    xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"',
    ),
)

# Deliberately no /OutputIntents, an independent defect that keeps a schema
# verdict visibly separate from it.
save(pdf, "extprop_2b.pdf", min_version="1.7")
