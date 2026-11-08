#!/usr/bin/env python3
"""Build inlinejpx_2b.pdf: inline images that apply JPXDecode or JBIG2Decode,
which no part of ISO 19005 after the first permits, for the check that states
the inline image filter rule and the repair that moves such an image out of
its content stream.

ISO 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid an inline image filter
that is LZW or outside the filters ISO 32000-2, 8.9.7, Table 92 abbreviates
(AHx, A85, LZW, Fl, RL, CCF and DCT, under either name). JPXDecode and
JBIG2Decode have no abbreviation, and ISO 32000-1, 7.4.9 applies the JPEG 2000
filter to image XObjects alone. ISO 19005-1, 6.1.10 names LZWDecode
alone, so part 1 judges none of these images by that rule. veraPDF judges each
inline image as an image of its own:

  page   P1 writes /F /JPXDecode with /CS /RGB and /BPC 8. P2 writes
         /F /JPXDecode with no /CS and no /BPC, the colour space read from
         the JPEG 2000 data (ISO 32000-1, 8.9.5, Table 89). P3 writes
         /F /JBIG2Decode, a 1-bit DeviceGray image. P4 writes
         /F [/AHx /JBIG2Decode] as an image mask. P5 writes /F /CCF and P6
         /F /Fl, which every part permits, and are the controls.
  Fm     a form XObject the page draws, whose image writes /Filter /JPXDecode,
         the full key, with /ColorSpace /DeviceRGB.

The JPEG 2000 data is the three-component codestream of _jp2.py in a JP2
container with one sRGB colour specification. The JBIG2 data is an embedded
stream (ISO 32000-1, 7.4.7) of a page information segment, an immediate
lossless generic region coded with MMR (ITU-T T.88, 6.2.6) and an end of page
segment, the same 8x8 diagonal band the CCF control codes with Group 4. No
image refers to JBIG2Globals.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import binascii
import struct
import zlib

import _jp2
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

# The 8x8 band, ITU-T T.6 (Group 4) coded with the band black, ending with
# EOFB.
G4 = bytes.fromhex("26bdbb76eddbb64d5380080080")
SIZE = 8


def segment(number, seg_type, data, page=1):
    """One JBIG2 segment, ITU-T T.88, 7.2: a header and its data."""
    return struct.pack(">IBBBI", number, seg_type, 0, page, len(data)) + data


def jbig2(mmr):
    """An embedded JBIG2 stream: page information (type 48), an immediate
    lossless generic region (type 39) and end of page (type 49)."""
    page_info = struct.pack(">IIIIBH", SIZE, SIZE, 0, 0, 0, 0)
    region = struct.pack(">IIIIB", SIZE, SIZE, 0, 0, 0) + bytes([1]) + mmr
    return segment(0, 48, page_info) + segment(1, 39, region) + segment(2, 49, b"")


JPX = _jp2.container(_jp2.THREE, 3, 8, [_jp2.colr(1, 1, _jp2.ENUM_SRGB)])
JB2 = jbig2(G4)
GREY = bytes([0x00, 0x40, 0x80, 0xFF, 0xFF, 0x80, 0x40, 0x00] * 8)


def inline(x, entries, data):
    # A reader finds the end of inline image data by scanning for EI, so no
    # planted payload may hold one.
    assert b"EI" not in data, entries
    return (
        b"q 40 0 0 40 " + str(x).encode() + b" 600 cm BI /W 8 /H 8 " + entries + b" ID " + data + b"\nEI Q\n"
    )


fm = Stream(pdf, inline(0, b"/ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /JPXDecode", JPX))
fm.Type = Name.XObject
fm.Subtype = Name.Form
fm.BBox = Array([0, 0, 100, 700])
fm.Resources = Dictionary()
fm = pdf.make_indirect(fm)

content = pdf.make_indirect(
    Stream(
        pdf,
        inline(10, b"/CS /RGB /BPC 8 /F /JPXDecode", JPX)
        + inline(60, b"/F /JPXDecode", JPX)
        + inline(110, b"/CS /G /BPC 1 /F /JBIG2Decode", JB2)
        + inline(160, b"/IM true /F [/AHx /JBIG2Decode]", binascii.hexlify(JB2).upper() + b">")
        + inline(210, b"/CS /G /BPC 1 /F /CCF /DP << /K -1 /Columns 8 >>", G4)
        + inline(260, b"/CS /G /BPC 8 /F /Fl", zlib.compress(GREY, 9))
        + b"q 1 0 0 1 300 0 cm /Fm Do Q\n",
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(XObject=Dictionary(Fm=fm)),
        Contents=content,
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "inlinejpx_2b.pdf", min_version="1.7")
