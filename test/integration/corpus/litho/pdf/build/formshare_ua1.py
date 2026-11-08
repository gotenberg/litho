#!/usr/bin/env python3
"""Build formshare_ua1.pdf: marked forms a content stream two pages share draws.

ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
a form's /StructParents names the one parent tree entry its marked-content
sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
1.30.2 states test 2 on the form object a Do builds, and parses a content
stream once for the document: a Do in a /Contents two pages share is one
reference, whether the second page reads it under the same resources or
under resources of its own. litho read the content of every page again and stated test 2 for a
marked form the shared content draws once. No corpus file shares a content
stream drawing a marked form at PDF/UA-1, so this fixture plants both
readings, each pair of pages drawing a marked form of its own:

| Page | Path                                                       | Answers at PDF/UA-1 |
| ---- | ---------------------------------------------------------- | ------------------- |
| 1, 2 | shared content, `/F Do`, page 2 under resources of its own | nothing             |
| 3, 4 | shared content, `/F Do`, one resource dictionary for both  | nothing             |

Every page is a must-not-fire control. Page 2's resources name the same form
as page 1's and add an ExtGState, so the two pages read the stream under
different resources.

A CMYK OutputIntent covers the DeviceGray every stream paints in.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

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


def form(content, **keys):
    """A form XObject with empty resources and the extra keys given."""
    xobj = Stream(pdf, content)
    xobj.Type = Name.XObject
    xobj.Subtype = Name.Form
    xobj.BBox = Array([0, 0, 20, 20])
    xobj.Resources = Dictionary()
    for key, value in keys.items():
        xobj[Name("/" + key)] = value
    return pdf.make_indirect(xobj)


def add_page(contents, resources):
    page = Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 200, 200]), Resources=resources, Contents=contents)
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


def shared_pair(name, xobject, same_resources):
    """Two pages whose /Contents is one stream drawing xobject under name."""
    contents = pdf.make_indirect(Stream(pdf, b"/" + name.encode() + b" Do\n"))
    first = pdf.make_indirect(Dictionary(XObject=Dictionary({"/" + name: xobject})))
    add_page(contents, first)
    if same_resources:
        add_page(contents, first)
    else:
        own = Dictionary(XObject=Dictionary({"/" + name: xobject}), ExtGState=Dictionary(G0=Dictionary(Type=Name.ExtGState)))
        add_page(contents, own)


MARKED = b"/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"

shared_pair("F", form(MARKED, StructParents=0), same_resources=False)
shared_pair("F", form(MARKED, StructParents=0), same_resources=True)

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "formshare_ua1.pdf")
