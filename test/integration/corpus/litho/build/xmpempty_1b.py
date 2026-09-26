#!/usr/bin/env python3
"""Build xmpempty_1b.pdf: empty XMP containers and control characters in /Info.

ISO 19005-1, 6.7.3 compares each present document information entry with its
XMP analogue, and veraPDF reads a container-typed analogue through its items:
XMPTitle and XMPDescription are the x-default item of the rdf:Alt, or its first
one, XMPCreator the first item of dc:creator and XMPCreatorSize how many items
it holds, an empty one included. A container with no item is null. Each shape
was measured on veraPDF 1.30.2 with a crafted PDF/A-1b file:

| Info entry              | XMP analogue                                   | Rule      |
| ----------------------- | ---------------------------------------------- | --------- |
| `/Title ()`             | `dc:title` an rdf:Alt with no item             | `6.7.3/2` |
| `/Author ()`            | `dc:creator` an empty item, then `x`           | `6.7.3/3` |
| `/Producer (\\003 v)`   | `pdf:Producer` ` v`, the control byte dropped  | `6.7.3/7` |
| `/Subject ()`           | `dc:description` one empty item, `xml:lang fr` | silent    |
| `/Creator (\\030 x)`    | `xmp:CreatorTool` `U+02D8 x`, the breve it is  | silent    |

The rdf:Alt with no item is a Lang Alt all the same, so dc:title passes 6.7.9
t3, and so does dc:rights, an empty rdf:Alt with no /Info entry beside it.

/Producer carries 0x03, a byte PDFDocEncoding leaves undefined. veraPDF reads it
as the control character it is, and no packet can hold one: XMPCore reads a
control character, raw or referenced, as a space. So no analogue equals the
entry. /Creator carries 0x18, which PDFDocEncoding maps to a breve, and equals
an analogue holding the breve.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

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

pdf.docinfo = pdf.make_indirect(
    Dictionary(
        Title=String(""),
        Author=String(""),
        Subject=String(""),
        Producer=String(b"\x03 v"),
        Creator=String(b"\x18 x"),
    )
)

XMP = """<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/">
   <pdfaid:part>1</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <!-- No item: null against the empty /Title. -->
   <dc:title><rdf:Alt/></dc:title>
   <!-- Two items: the first equals the empty /Author, the size does not. -->
   <dc:creator><rdf:Seq><rdf:li/><rdf:li>x</rdf:li></rdf:Seq></dc:creator>
   <!-- One empty item with no x-default: the first item, equal. -->
   <dc:description><rdf:Alt><rdf:li xml:lang="fr"/></rdf:Alt></dc:description>
   <!-- An rdf:Alt with no item, no /Info entry beside it. -->
   <dc:rights><rdf:Alt/></dc:rights>
   <pdf:Producer> v</pdf:Producer>
   <xmp:CreatorTool>˘ x</xmp:CreatorTool>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "xmpempty_1b.pdf", min_version="1.4", preserve_pdfa=False, fix_metadata_version=False)
