#!/usr/bin/env python3
"""Build sharedicc_2b.pdf: one content stream four pages share, each page
mapping its colour space name to an ICCBased space of its own.

ISO 32000-1, 7.7.3.3 lets pages share a content stream, and the names its
operators use resolve in each page's own /Resources (7.8.3), so one stream
paints a different colour space on each page. Merging duplicate streams on a
write, litho's --compact among them, makes such files: csnames_2b.pdf, so
rewritten, shares one stream between five pages and another between six.
veraPDF judges a shared stream once, through the first page's resources, and
judges none of the spaces a later page maps; litho judges each page whose
resources differ.

Every page fills with /CS0 through the one stream:

| Page | /CS0                                                      | Answers         |
| ---- | --------------------------------------------------------- | --------------- |
| 1    | ICCBased over a version 2 display profile                 | nothing         |
| 2    | ICCBased over a version 2 abstract profile                | 6.2.4.2 t1      |
| 3    | ICCBased over a version 4 display profile                 | nothing at 2b   |
| 4    | page 1's space, in resources equal to page 1's            | nothing         |

ISO 19005-2, 6.2.4.2 t1 takes an ICC profile of the input, output, display
or colour space class, so the abstract one fails at every PDF/A level. PDF/A-2
and later permit ICC version 4, so page 3 fails only at PDF/A-1, whose ISO
19005-1, 6.2.3.2 reads the PDF 1.4 profile, version 2. Page 4 is the control:
it reads the stream under the same resources as page 1, and is judged once
with it.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000
ICC_VERSION_4_2 = 0x04200000


def profile(device_class, version):
    """A header-only RGB profile of the given class and version."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", version)
    header[12:16] = device_class
    header[16:20] = b"RGB "
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = 3
    return pdf.make_indirect(stream)


def space(device_class, version):
    return pdf.make_indirect(Array([Name.ICCBased, profile(device_class, version)]))


content = pdf.make_indirect(Stream(pdf, b"/CS0 cs 0.5 0.5 0.5 scn 0 0 10 10 re f\n"))


def page(cs):
    d = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(ColorSpace=Dictionary(CS0=cs)),
        Contents=content,
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(d)))


display = space(b"mntr", ICC_VERSION_2_1)
page(display)
page(space(b"abst", ICC_VERSION_2_1))
page(space(b"mntr", ICC_VERSION_4_2))
page(display)

# Deliberately no /Metadata and no output intent.
save(pdf, "sharedicc_2b.pdf")
