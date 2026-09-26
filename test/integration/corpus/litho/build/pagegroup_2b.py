#!/usr/bin/env python3
"""Build pagegroup_2b.pdf: page transparency groups and a page XMP packet.

ISO 19005-2, 6.2.10 t2 asks every page that contains transparency, in a
document without a PDF/A OutputIntent, for a /Group whose /CS gives the
default blending colour space. The file declares no OutputIntent, and four
pages paint through a graphics state with constant alpha 0.5, in an ICCBased
space so no page uses a device colour space of its own:

| Page | /Group                                | Rule at PDF/A-2b |
| ---- | ------------------------------------- | ---------------- |
| 1    | none                                  | `6.2.10/2`       |
| 2    | `/S /Transparency /CS [/ICCBased ..]` | silent           |
| 3    | `/S /Transparency`, no `/CS`          | `6.2.10/2`       |
| 4    | `/S /Transparency /CS /DeviceRGB`     | `6.2.4.3/2`      |

Page 2 is the control: its ICCBased blending space answers the rule, and a
repair of pages 1 and 3 has to leave it in place. Page 4 blends in DeviceRGB,
which ISO 19005-2, 6.2.4.3 t2 allows only under an RGB OutputIntent.

At PDF/A-1b every one of those groups is a transparency group ISO 19005-1,
6.4 t3 forbids, and page 4's DeviceRGB blending space fails 6.2.3.3 t1 as
well: a repair that removes the groups must not synthesize them again.

Page 1 also carries its own XMP packet under /Metadata. ISO 19005-2,
6.6.2.3.1 constrains the properties of every metadata stream in the file, not
only the catalog's: photoshop:History is no property of the XMP 2005
Photoshop schema, which fails t1 and t2, and dc:format is written as a
language alternative where the schema defines MIMEType text, which fails t2.
xmp:CreatorTool is a predefined text property and stays silent. Part 1 judges
the catalog packet alone, so the page packet is silent at PDF/A-1b.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(colour_space, components, tag):
    """A structurally minimal ICC profile veraPDF will parse.

    veraPDF reads profile bytes eagerly and throws on a placeholder, so the
    header carries a real size, an `acsp` signature and a PCS illuminant.
    """
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    body = bytes(header) + tag_table
    stream = Stream(pdf, body)
    stream.N = components
    return pdf.make_indirect(stream)


iccbased = pdf.make_indirect(Array([Name.ICCBased, icc_profile(b"RGB ", 3, b"ICCB")]))

PAGE_PACKET = (
    '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
    '<x:xmpmeta xmlns:x="adobe:ns:meta/">\n'
    ' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
    '  <rdf:Description rdf:about=""\n'
    '    xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/"\n'
    '    xmlns:xmp="http://ns.adobe.com/xap/1.0/"\n'
    '    xmlns:dc="http://purl.org/dc/elements/1.1/">\n'
    '   <photoshop:History>Scanned, then cropped</photoshop:History>\n'
    '   <dc:format><rdf:Alt><rdf:li xml:lang="x-default">image/tiff</rdf:li></rdf:Alt></dc:format>\n'
    '   <xmp:CreatorTool>litho corpus scanner</xmp:CreatorTool>\n'
    '  </rdf:Description>\n'
    ' </rdf:RDF>\n'
    '</x:xmpmeta>\n'
    '<?xpacket end="w"?>\n'
).encode("utf-8")

# Every page paints the same square through constant alpha, in the ICCBased
# space, so each page contains transparency and none uses a device space.
CONTENT = b"/GS0 gs /CS0 cs 0.2 0.4 0.6 scn 60 60 200 200 re f\n"


def add_page(group=None, metadata=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            ExtGState=Dictionary(GS0=Dictionary(Type=Name.ExtGState, ca=0.5)),
            ColorSpace=Dictionary(CS0=iccbased),
        ),
        Contents=Stream(pdf, CONTENT),
    )
    if group is not None:
        page.Group = group
    if metadata is not None:
        page.Metadata = metadata
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


page_meta = Stream(pdf, PAGE_PACKET)
page_meta.Type = Name.Metadata
page_meta.Subtype = Name.XML

add_page(metadata=pdf.make_indirect(page_meta))
add_page(group=Dictionary(Type=Name.Group, S=Name.Transparency, CS=iccbased))
add_page(group=Dictionary(Type=Name.Group, S=Name.Transparency))
add_page(group=Dictionary(Type=Name.Group, S=Name.Transparency, CS=Name.DeviceRGB))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "pagegroup_2b.pdf", min_version="1.7")
