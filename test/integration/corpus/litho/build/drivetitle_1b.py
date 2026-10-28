#!/usr/bin/env python3
"""Build drivetitle_1b.pdf: document information text that begins like a PDF
date, at PDF/A-1b.

ISO 19005-1, 6.7.3 makes each document information entry equal to its XMP
analogue, and veraPDF compares a text entry as a string ("Title == XMPTitle").
Only /CreationDate and /ModDate are dates (ISO 32000-1, Table 317). litho read
every Info value that starts with "D:" as a date, on the Info side alone, so a
/Title naming a Windows path on drive D failed against its own copy in
dc:title. No file of the litho or veraPDF corpora reaches it; the facturx and
govdocs files that do are not remediated by the gate.

Three entries, each equal to its XMP analogue, so no 6.7.3 test fires:

| Info entry      | Value                      | XMP property      |
| --------------- | -------------------------- | ----------------- |
| `/Title`        | `D:\\openkm\\temp\\okm25.pdf` | `dc:title`     |
| `/Subject`      | `D:20200101`, date-shaped  | `dc:description`  |
| `/CreationDate` | `D:20200101120000Z`, a date | `xmp:CreateDate` |

/Title and /Subject are the two probes, on independent tests (6.7.3 t2 and
t4), so a check that latches on the first cannot hide the second.
/CreationDate is the control: a real date still compares as an instant, so
"2020-01-01T12:00:00Z" matches it.

The file is otherwise a conforming PDF/A-1b document, built as
iccv4intent_1b.pdf with a v2 CMYK destination profile.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, version, tag):
    """A structurally minimal ICC profile, as iccbased_4.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", version)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", ICC_VERSION_2_1, b"OUTI"))
destination.N = 4
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoBareSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoBareSans"),
        FirstChar=0x41,
        LastChar=0x42,
        Widths=Array([_truetype.ADVANCE_WIDTH] * 2),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, b"BT /F1 24 Tf 20 100 Td (AB) Tj ET\n")),
    )
)
pdf.pages.append(pikepdf.Page(page))

TITLE = "D:\\openkm\\temp\\okm25.pdf"
SUBJECT = "D:20200101"
pdf.docinfo = pdf.make_indirect(
    Dictionary(
        Title=String(TITLE),
        Subject=String(SUBJECT),
        CreationDate=String("D:20200101120000Z"),
    )
)

XMP = """<?xpacket begin="\ufeff" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:dc="http://purl.org/dc/elements/1.1/">
   <pdfaid:part>1</pdfaid:part>
   <pdfaid:conformance>B</pdfaid:conformance>
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:title>
   <dc:description><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:description>
   <xmp:CreateDate>2020-01-01T12:00:00Z</xmp:CreateDate>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>
""" % (TITLE, SUBJECT)

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "drivetitle_1b.pdf", min_version="1.4", fix_metadata_version=False)
