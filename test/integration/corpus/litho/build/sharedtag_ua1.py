#!/usr/bin/env python3
"""Build sharedtag_ua1.pdf: untagged text and a marked form two pages share.

ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
a form's /StructParents names the one parent tree entry its marked-content
sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
1.30.2 parses a content stream once for the document, so a Do in a /Contents
two pages share is one reference. Remediation's auto-tagger marked each page's
content in a stream of its own, so the form gained a second reference after
tagging. formshare_ua1.pdf reaches the content-tag wrapper, which tags pages
with no text; this fixture reaches the auto-tagger, which tags pages that show
text:

| Page | Path                                                     | Answers at PDF/UA-1 |
| ---- | -------------------------------------------------------- | ------------------- |
| 1, 2 | shared content, Type 3 text then `/F Do`, one resources  | 7.1 t3 (untagged)   |
| 3    | content of its own, the same text, no form               | 7.1 t3 (untagged)   |

Every page is a must-not-fire control for 7.20 t2, before and after
remediation. The Type 3 font needs no embedding (ISO 14289-1, 7.21.4.1 names
Type 3 fonts out of scope) and carries a ToUnicode CMap.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    tounicode_cmap,
    type3_font,
    xmp_packet,
)

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as patternglyph_2b.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", b"OUTI"))
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

font = type3_font(
    pdf,
    [("A", 600, "0 0 500 700 re f"), ("B", 600, "0 0 500 700 re f")],
    [600, 600],
)
font.ToUnicode = tounicode_cmap(pdf, {65: 0x41, 66: 0x42})
font = pdf.make_indirect(font)

marked = Stream(pdf, b"/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n")
marked.Type = Name.XObject
marked.Subtype = Name.Form
marked.BBox = Array([0, 0, 20, 20])
marked.Resources = Dictionary()
marked.StructParents = 0
marked = pdf.make_indirect(marked)

TEXT = b"BT /T 24 Tf 20 150 Td (ABABBABA) Tj ET\n"


def add_page(contents, resources):
    page = Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 200, 200]), Resources=resources, Contents=contents)
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


shared = pdf.make_indirect(Stream(pdf, TEXT + b"/F Do\n"))
resources = pdf.make_indirect(Dictionary(Font=Dictionary(T=font), XObject=Dictionary(F=marked)))
add_page(shared, resources)
add_page(shared, resources)
add_page(pdf.make_indirect(Stream(pdf, TEXT)), Dictionary(Font=Dictionary(T=font)))

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "sharedtag_ua1.pdf")
