#!/usr/bin/env python3
"""Build sharedclose_ua1.pdf: a marked form and an artifact sequence left open,
in a content stream two pages share, at PDF/UA-1.

ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
a form's /StructParents names the one parent tree entry its marked-content
sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
1.30.2 parses a content stream once for the document, so a Do in a /Contents
two pages share is one reference. Remediation closes a marked-content
sequence the page content leaves open (ISO 32000-1, 14.6) in a stream
appended to the page's /Contents, and gave each page an array of its own
naming the shared stream, so veraPDF parsed the stream once for each array and
the form gained a second reference. formshare_ua1.pdf and sharedtag_ua1.pdf
reach the content-tag wrapper and the auto-tagger; this fixture reaches the
fixer closing open sequences, which no corpus file reaches on shared content:

| Page | Path                                                      | Answers at PDF/UA-1 |
| ---- | --------------------------------------------------------- | ------------------- |
| 1, 2 | shared content, `/F Do`, then an `/Artifact` BMC unclosed | 7.1 t3 (unmarked)   |
| 3    | content of its own, the `/Artifact` BMC unclosed, no form | 7.1 t3 (unmarked)   |

Every page is a must-not-fire control for 7.20 t2, before and after
remediation. The form's marked content belongs to a /P the structure tree
reaches through the form's /StructParents. The document is otherwise tagged:
/MarkInfo, /Lang, DisplayDocTitle and an XMP packet carrying the pdfuaid
claim.

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

marked = Stream(pdf, b"/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n")
marked.Type = Name.XObject
marked.Subtype = Name.Form
marked.BBox = Array([0, 0, 20, 20])
marked.Resources = Dictionary()
marked.StructParents = 0
marked = pdf.make_indirect(marked)

OPEN = b"/Artifact BMC\n0 G 20 90 m 180 90 l S\n"


def add_page(contents, resources):
    page = Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 200, 200]), Resources=resources, Contents=contents)
    page = pdf.make_indirect(page)
    pdf.pages.append(pikepdf.Page(page))
    return page


shared = pdf.make_indirect(Stream(pdf, b"/F Do\n" + OPEN))
resources = pdf.make_indirect(Dictionary(XObject=Dictionary(F=marked)))
first = add_page(shared, resources)
add_page(shared, resources)
add_page(pdf.make_indirect(Stream(pdf, OPEN)), Dictionary())

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))
paragraph = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.P,
        P=document,
        Pg=first,
        K=Dictionary(Type=Name.MCR, Pg=first, Stm=marked, MCID=0),
    )
)
document.K = Array([paragraph])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array([paragraph])])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "sharedclose_ua1.pdf", fix_metadata_version=False)
