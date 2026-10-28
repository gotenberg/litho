#!/usr/bin/env python3
"""Build sharedstream_2b.pdf: one content stream two pages read differently.

ISO 32000-1, 7.7.3.3 lets two pages name the same content stream, and each
page reads it through its own /Resources (7.8.3). Both pages here name one
stream as their whole /Contents:

  BT /F1 24 Tf 50 700 Td (A\\x01) Tj ET

  page 1  /F1 is a simple TrueType under WinAnsiEncoding: `A` shows A, and
          0x01 has no glyph, so it shows `.notdef` (ISO 19005-2, 6.2.11.8 t1)
  page 2  /F1 is a Type0 font under Identity-H: the two bytes are one code,
          0x4101, CID 16641, which its CIDToGIDMap sends to the glyph A

The aggressive text-control repair strips the 0x01 page 1 shows. Rewriting the
shared stream in place took the byte off page 2 as well, leaving it a lone
`A` that completes no two-byte code: page 2's glyph was gone. The page the
repair rewrites now gets a stream of its own, and page 2 keeps its code.

No corpus file has a page that names, through a reference, a stream another
page names too and that a content repair rewrites, so the gate never saw it.

Both fonts embed the conforming TrueType program of `ttcmap_2b.pdf`. The simple
font's `/Widths` cover the codes from 1, so the width of the `.notdef` glyph
agrees with the dictionary, and the Type0 font carries a ToUnicode CMap so its
code maps to U+0041.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import zlib

import _truetype
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])


def font_file():
    stream = Stream(pdf, program)
    stream.Length1 = len(program)
    return pdf.make_indirect(stream)


def descriptor(name):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name(name),
            Flags=32,  # non-symbolic, ISO 32000-1, Table 123
            ItalicAngle=0,
            Ascent=800,
            Descent=-200,
            CapHeight=700,
            StemV=80,
            FontBBox=Array([0, -200, 550, 700]),
            FontFile2=font_file(),
        )
    )


FIRST, LAST = 1, 66
simple = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoSharedSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=descriptor("/LithoSharedSans"),
        Encoding=Name.WinAnsiEncoding,
    )
)

CID = 0x4101  # the bytes "A\x01" read as one Identity-H code
GID_A = 1
gid_map = bytearray(2 * (CID + 1))
gid_map[2 * CID : 2 * CID + 2] = GID_A.to_bytes(2, "big")
cid_to_gid = Stream(pdf, zlib.compress(bytes(gid_map), 9))
cid_to_gid.Filter = Name.FlateDecode

to_unicode = Stream(
    pdf,
    b"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n"
    b"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n"
    b"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n"
    b"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
    b"1 beginbfchar\n<4101> <0041>\nendbfchar\n"
    b"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n",
)

composite = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type0,
        BaseFont=Name("/LithoSharedCID"),
        Encoding=Name("/Identity-H"),
        ToUnicode=pdf.make_indirect(to_unicode),
        DescendantFonts=Array(
            [
                pdf.make_indirect(
                    Dictionary(
                        Type=Name.Font,
                        Subtype=Name.CIDFontType2,
                        BaseFont=Name("/LithoSharedCID"),
                        CIDSystemInfo=Dictionary(
                            Registry=pikepdf.String("Adobe"),
                            Ordering=pikepdf.String("Identity"),
                            Supplement=0,
                        ),
                        FontDescriptor=descriptor("/LithoSharedCID"),
                        DW=1000,
                        W=Array([CID, Array([_truetype.ADVANCE_WIDTH])]),
                        CIDToGIDMap=pdf.make_indirect(cid_to_gid),
                    )
                )
            ]
        ),
    )
)

shared = pdf.make_indirect(Stream(pdf, b"BT /F1 24 Tf 50 700 Td (A\x01) Tj ET\n"))

for font in (simple, composite):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=shared,
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "sharedstream_2b.pdf", min_version="1.7")
