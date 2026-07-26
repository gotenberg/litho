#!/usr/bin/env python3
"""Build idprefix_2b.pdf: namespace prefixes on the PDF/A Identification Schema.

The coverage scan showed ISO 19005-2, 6.6.4 tests 4 to 7 unreached. Each one
names a single property of the identification schema and requires it to carry
the prefix `pdfaid`. The whole cluster is XMP text, so it costs nothing to
plant, and it is a different axis from the extension-schema prefixes
`schemaprefix_1b.pdf` covers: this one governs the schema that declares the
file's conformance in the first place.

All four properties carry the wrong prefix at once, so a check that reports
the first misprefixed property and stops reports one rule where four are due.
The prefix verdict is per property rather than per schema, which is measured
rather than assumed: a packet with `pdfaid:part` correct and the rest wrong
reports tests 5 to 7 and stays silent on 4, on both tools.

`amd` and `corr` are optional properties, so a file has to declare them to
reach tests 6 and 7 at all. They are declared here for that reason.

Two independent defects sit beside the prefixes: a `/Metadata` stream on page
2 whose packet header carries a `bytes` attribute, and an ExtGState with a
transfer function. Neither belongs to 6.6.4, so they show that a prefix
verdict on the identification schema does not leak into unrelated clauses.

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

transfer = Stream(pdf, b"{ 1 exch sub }")
transfer.FunctionType = 4
transfer.Domain = Array([0, 1])
transfer.Range = Array([0, 1])
gs = pdf.make_indirect(
    Dictionary(Type=Name.ExtGState, TR=pdf.make_indirect(transfer))
)

page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font), ExtGState=Dictionary(GS1=gs)),
        Contents=Stream(
            pdf,
            b"q /GS1 gs 0 0 0 rg 50 700 60 30 re f Q\n"
            b"BT /F1 12 Tf 50 660 Td (identification prefixes) Tj ET\n",
        ),
    )
)
pdf.pages.append(pikepdf.Page(page1))

# Page 2 carries its own metadata stream. Its packet header declares `bytes`,
# which ISO 19005-2, 6.6.2.1 test 2 forbids in any XMP packet, not only the
# catalog's. It is here so the fixture reaches a packet rule through a
# non-catalog stream while the identification prefixes are also wrong.
page2_meta = Stream(
    pdf,
    b'<?xpacket begin="\xef\xbb\xbf" id="W5M0MpCehiHzreSzNTczkc9d" bytes="1024"?>'
    b'<x:xmpmeta xmlns:x="adobe:ns:meta/">'
    b'<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
    b'<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">'
    b"<dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">page two</rdf:li>"
    b"</rdf:Alt></dc:title></rdf:Description>"
    b'</rdf:RDF></x:xmpmeta><?xpacket end="w"?>',
)
page2_meta.Type = Name.Metadata
page2_meta.Subtype = Name.XML

page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (page two) Tj ET\n"),
        Metadata=pdf.make_indirect(page2_meta),
    )
)
pdf.pages.append(pikepdf.Page(page2))

# `xid` is bound to the identification namespace, so every property resolves
# correctly and only the prefix is wrong. The conforming `dc` and `xmp`
# properties beside them, and the fully conforming extension schema below,
# are the must-not-fire controls.
packet = (
    '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>'
    '<x:xmpmeta xmlns:x="adobe:ns:meta/">'
    '<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
    '<rdf:Description rdf:about=""'
    ' xmlns:xid="http://www.aiim.org/pdfa/ns/id/">'
    "<xid:part>2</xid:part>"
    "<xid:conformance>B</xid:conformance>"
    "<xid:amd>2005</xid:amd>"
    "<xid:corr>1</xid:corr>"
    "</rdf:Description>"
    '<rdf:Description rdf:about=""'
    ' xmlns:dc="http://purl.org/dc/elements/1.1/"'
    ' xmlns:xmp="http://ns.adobe.com/xap/1.0/">'
    '<dc:title><rdf:Alt><rdf:li xml:lang="x-default">identification prefixes'
    "</rdf:li></rdf:Alt></dc:title>"
    "<xmp:CreatorTool>litho corpus builder</xmp:CreatorTool>"
    "</rdf:Description>"
    '<rdf:Description rdf:about=""'
    ' xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"'
    ' xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"'
    ' xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#">'
    "<pdfaExtension:schemas><rdf:Bag><rdf:li rdf:parseType=\"Resource\">"
    "<pdfaSchema:schema>Litho Control Schema</pdfaSchema:schema>"
    "<pdfaSchema:namespaceURI>http://example.org/litho/control#"
    "</pdfaSchema:namespaceURI>"
    "<pdfaSchema:prefix>lctl</pdfaSchema:prefix>"
    "<pdfaSchema:property><rdf:Seq><rdf:li rdf:parseType=\"Resource\">"
    "<pdfaProperty:name>controlOne</pdfaProperty:name>"
    "<pdfaProperty:valueType>Text</pdfaProperty:valueType>"
    "<pdfaProperty:category>external</pdfaProperty:category>"
    "<pdfaProperty:description>conforming in every field</pdfaProperty:description>"
    "</rdf:li></rdf:Seq></pdfaSchema:property>"
    "</rdf:li></rdf:Bag></pdfaExtension:schemas>"
    "</rdf:Description>"
    '</rdf:RDF></x:xmpmeta><?xpacket end="w"?>'
).encode("utf-8")

meta = Stream(pdf, packet)
meta.Type = Name.Metadata
meta.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(meta)

# Deliberately no /OutputIntents, so the colour rules stay reachable too.
save(pdf, "idprefix_2b.pdf", min_version="1.7")
