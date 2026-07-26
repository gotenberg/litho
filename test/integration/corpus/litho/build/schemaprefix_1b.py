#!/usr/bin/env python3
"""Build schemaprefix_1b.pdf: extension schema containers and absent fields.

`extschema_2b.pdf` reaches six tests of the extension schema clause, all of
them by omitting a required field from a schema, a property or a field. The
clause is numbered 6.7.8 in ISO 19005-1 and 6.6.2.3.3 in ISO 19005-2/3, and
the same content answers both, so one fixture closes both cells.

Twelve tests stayed unreached because they need the other halves of the same
conjunctions. Each test reads `isXValid... == true && ...Prefix == "pdfaN"`,
so a test fires on a wrong RDF container type or an absent field just as it
does on a wrong namespace prefix. This fixture takes the container and the
absence route:

  * The `pdfaExtension:schemas` container is an `rdf:Seq`, which t2 rejects.
  * `pdfaSchema:valueType` and `pdfaType:field` carry an `rdf:Bag` where t7
    and t16 require an `rdf:Seq`.
  * Eight entries each drop one required field, reaching t3, t4, t9, t12, t13,
    t14, t18 and t19.
  * One schema entry carries a field outside the extension schema vocabulary,
    reaching t1.

Seven schema entries sit in the one container, and the value types and fields
of the sixth are siblings inside it. That grouping is the masking probe: four
sibling value types carry one defect each, a fifth carries the two defective
fields, and a sixth conforms, so a check that reports the first offending type
and stops reports one rule where four are due, and a check that condemns a
whole `rdf:Seq` once a member fails wrongly reports the conforming sixth type
and the conforming seventh schema entry.

Deliberately unreached, with the evidence in ../README.md:

  * t6, the property container. veraPDF reports it for an `rdf:Bag` or a bare
    literal in place of the `rdf:Seq`; litho reports neither, so planting it
    would add a disagreeing cell rather than coverage.
  * The wrong-prefix half of every test. litho answers a single wrong prefix
    with a blanket report of every rule at that level and skips the rest of
    the packet, so one prefix defect both over-reports and masks the other
    eleven.

The page shows a line of text rather than nothing. An empty content stream
presents no text item, which is the subject of ISO 14289-1, 7.2 t34, and litho
reports that test from the absence of a structure tree alone. One line of text
keeps the two tools agreed on every profile so the clause under test is the
only thing this fixture says.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

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


def li(inner):
    """Wrap one extension schema definition as an rdf:li resource."""
    return '<rdf:li rdf:parseType="Resource">' + inner + "</rdf:li>"


def prop(name, value_type, category, description):
    """One pdfaProperty rdf:li. Any argument may be None to omit that field."""
    parts = []
    if name is not None:
        parts.append(f"<pdfaProperty:name>{name}</pdfaProperty:name>")
    if value_type is not None:
        parts.append(f"<pdfaProperty:valueType>{value_type}</pdfaProperty:valueType>")
    if category is not None:
        parts.append(f"<pdfaProperty:category>{category}</pdfaProperty:category>")
    if description is not None:
        parts.append(
            f"<pdfaProperty:description>{description}</pdfaProperty:description>"
        )
    return li("".join(parts))


def field(name, value_type, description):
    """One pdfaField rdf:li inside a value type. None omits that field."""
    parts = []
    if name is not None:
        parts.append(f"<pdfaField:name>{name}</pdfaField:name>")
    if value_type is not None:
        parts.append(f"<pdfaField:valueType>{value_type}</pdfaField:valueType>")
    if description is not None:
        parts.append(f"<pdfaField:description>{description}</pdfaField:description>")
    return li("".join(parts))


def value_type(type_name, uri, prefix, description, fields=(), field_tag="rdf:Seq"):
    """One pdfaType rdf:li. field_tag names the container its fields sit in."""
    parts = []
    if type_name is not None:
        parts.append(f"<pdfaType:type>{type_name}</pdfaType:type>")
    if uri is not None:
        parts.append(f"<pdfaType:namespaceURI>{uri}</pdfaType:namespaceURI>")
    if prefix is not None:
        parts.append(f"<pdfaType:prefix>{prefix}</pdfaType:prefix>")
    if description is not None:
        parts.append(f"<pdfaType:description>{description}</pdfaType:description>")
    if fields:
        parts.append(
            f"<pdfaType:field><{field_tag}>"
            + "".join(fields)
            + f"</{field_tag}></pdfaType:field>"
        )
    return li("".join(parts))


def schema(
    name, uri, prefix, props, value_types=(), value_type_tag="rdf:Seq", extra=""
):
    """One pdfaSchema rdf:li. value_type_tag names its value type container."""
    parts = []
    if name is not None:
        parts.append(f"<pdfaSchema:schema>{name}</pdfaSchema:schema>")
    if uri is not None:
        parts.append(f"<pdfaSchema:namespaceURI>{uri}</pdfaSchema:namespaceURI>")
    if prefix is not None:
        parts.append(f"<pdfaSchema:prefix>{prefix}</pdfaSchema:prefix>")
    parts.append(
        "<pdfaSchema:property><rdf:Seq>"
        + "".join(props)
        + "</rdf:Seq></pdfaSchema:property>"
    )
    if value_types:
        parts.append(
            f"<pdfaSchema:valueType><{value_type_tag}>"
            + "".join(value_types)
            + f"</{value_type_tag}></pdfaSchema:valueType>"
        )
    return li("".join(parts) + extra)


def plain(name):
    """One conforming property, so a schema's defect stays where it is put."""
    return prop(name, "Text", "external", "conforming property")


# t3: no pdfaSchema:schema. The rest of the entry conforms, so the report is
# attributable to the absent name alone.
s_noschema = schema(
    None,
    "http://example.org/litho/delta#",
    "ldelta",
    [plain("deltaOne")],
)

# t4: no pdfaSchema:namespaceURI.
s_nouri = schema(
    "Litho Epsilon Schema",
    None,
    "lepsilon",
    [plain("epsilonOne")],
)

# t1: a field the PDF/A extension schema vocabulary does not define. The
# decision is by namespace URI: litho:extra belongs to no pdfa namespace, so
# it is an undefined field rather than a misprefixed one.
s_alien = schema(
    "Litho Zeta Schema",
    "http://example.org/litho/zeta#",
    "lzeta",
    [plain("zetaOne")],
    extra="<litho:extra>outside the extension schema vocabulary</litho:extra>",
)

# t7: the value type container is an rdf:Bag where the clause requires an
# rdf:Seq. Its single value type conforms, so only the container fails.
s_vtbag = schema(
    "Litho Eta Schema",
    "http://example.org/litho/eta#",
    "leta",
    [prop("etaOne", "EtaRecord", "internal", "uses the type below")],
    [
        value_type(
            "EtaRecord",
            "http://example.org/litho/eta/record#",
            "erec",
            "conforming value type in a wrong container",
        )
    ],
    value_type_tag="rdf:Bag",
)

# t9: a property with no pdfaProperty:valueType, beside a conforming sibling.
# The sibling is the control: a check that condemns the property Seq once a
# member fails would report it too.
s_novt = schema(
    "Litho Theta Schema",
    "http://example.org/litho/theta#",
    "ltheta",
    [
        prop("thetaOne", None, "external", "this property declares no value type"),
        plain("thetaTwo"),
    ],
)

# t12, t13, t14, t16, t18 and t19 all live below a value type, so six sibling
# types carry one defect each. Every property of this schema uses the built-in
# Text type: a property pointing at one of these types would add veraPDF's
# "value type not defined" report to the type that drops its own name.
s_types = schema(
    "Litho Iota Schema",
    "http://example.org/litho/iota#",
    "liota",
    [plain("iotaOne")],
    [
        # t12: no pdfaType:type.
        value_type(
            None,
            "http://example.org/litho/iota/one#",
            "ione",
            "value type with no name",
        ),
        # t13: no pdfaType:namespaceURI.
        value_type(
            "IotaTwo",
            None,
            "itwo",
            "value type with no namespace URI",
        ),
        # t14: no pdfaType:prefix.
        value_type(
            "IotaThree",
            "http://example.org/litho/iota/three#",
            None,
            "value type with no prefix",
        ),
        # t16: the field container is an rdf:Bag where an rdf:Seq is required.
        # Its field conforms, so only the container fails.
        value_type(
            "IotaFour",
            "http://example.org/litho/iota/four#",
            "ifour",
            "fields in a wrong container",
            [field("fourOne", "Text", "conforming field")],
            field_tag="rdf:Bag",
        ),
        # t18 and t19: two defective fields and one conforming one, siblings
        # in the same rdf:Seq.
        value_type(
            "IotaFive",
            "http://example.org/litho/iota/five#",
            "ifive",
            "value type whose fields carry the defects",
            [
                field("fiveOne", None, "this field declares no value type"),
                field("fiveTwo", "Text", None),
                field("fiveThree", "Text", "conforming field"),
            ],
        ),
        # Control: conforming in every respect its five siblings break.
        value_type(
            "IotaSix",
            "http://example.org/litho/iota/six#",
            "isix",
            "fully conforming value type",
            [field("sixOne", "Text", "conforming field")],
        ),
    ],
)

# Control: a schema entry conforming in every field, whose property names a
# value type this same entry defines, whose value type conforms and whose
# field conforms. Its silence is what shows the container defect above is not
# condemning the whole bag.
s_ok = schema(
    "Litho Kappa Schema",
    "http://example.org/litho/kappa#",
    "lkappa",
    [prop("kappaOne", "KappaRecord", "external", "names the type below")],
    [
        value_type(
            "KappaRecord",
            "http://example.org/litho/kappa/record#",
            "krec",
            "fully conforming value type",
            [field("kappaFieldOne", "Text", "conforming field")],
        )
    ],
)

# t2: the container is an rdf:Seq where ISO 19005-1 6.7.8 requires an rdf:Bag.
# veraPDF still descends into the entries, so the container defect and the
# entry defects are reported side by side rather than one hiding the other.
packet = (
    '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>'
    '<x:xmpmeta xmlns:x="adobe:ns:meta/">'
    '<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
    '<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">'
    "<pdfaid:part>1</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance>"
    "</rdf:Description>"
    '<rdf:Description rdf:about=""'
    ' xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"'
    ' xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"'
    ' xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"'
    ' xmlns:pdfaType="http://www.aiim.org/pdfa/ns/type#"'
    ' xmlns:pdfaField="http://www.aiim.org/pdfa/ns/field#"'
    ' xmlns:litho="http://example.org/litho/alien#">'
    "<pdfaExtension:schemas><rdf:Seq>"
    + s_noschema
    + s_nouri
    + s_alien
    + s_vtbag
    + s_novt
    + s_types
    + s_ok
    + "</rdf:Seq></pdfaExtension:schemas>"
    "</rdf:Description>"
    "</rdf:RDF></x:xmpmeta>"
    '<?xpacket end="w"?>'
).encode("utf-8")

meta = Stream(pdf, packet)
meta.Type = Name.Metadata
meta.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(meta)

save(pdf, "schemaprefix_1b.pdf", min_version="1.4")
