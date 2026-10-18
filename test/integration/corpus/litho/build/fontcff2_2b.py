#!/usr/bin/env python3
"""Build fontcff2_2b.pdf: OpenType programs read by the font that embeds them.

ISO 32000-1, Table 126 lets /FontFile3 /OpenType hold an OpenType program
(PDF 1.6). veraPDF reads it by the font that embeds it, not by the outlines it
carries: a Type 1 or CIDFontType0 font as a CFF font through its 'CFF ' table,
a TrueType or CIDFontType2 font as a TrueType font through its 'hhea' and
'hmtx' tables. A program lacking the tables its reading needs is not embedded
(ISO 19005-2, 6.2.11.4.1 t1), and a 'CFF2' table (OpenType 1.8) is not a
'CFF ' table. Each simple font shows codes 41 and 42 through
/WinAnsiEncoding, each Type 0 font CIDs 1 and 2 under Identity-H:

  F1  Type 1, an "OTTO" whose outlines are 'CFF2'.
  F2  CIDFontType0, the same program.
  F3  TrueType, the same program, /Widths 600 as 'hmtx' states.
  F4  CIDFontType2, the same program, /W 600.
  F5  TrueType, the same program, /Widths 500.
  M1  Type 1, a TrueType-outline sfnt (version 1.0), no 'CFF ' table.
  M2  CIDFontType0, the same sfnt.
  M3  TrueType, an "OTTO" with a 'CFF ' table and no 'hhea' or 'hmtx'.
  M4  CIDFontType2, the same program.

F1, F2, M1 to M4 are not embedded. F3 and F4 are, and F5 fails the width rule
alone. OK, a Type 1 font over a whole "OTTO" with 'CFF ' outlines, is the
control.

See ../README.md for the authoritative expected firing set.
"""

import struct

import _cff
import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()
NONSYMBOLIC = 32
SYMBOLIC = 4

tt = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])
tables = _truetype.tables_of(tt)
maxp05 = struct.pack(">IH", 0x00005000, len(_truetype.GLYPH_NAMES))


def otto(outlines, metrics=True):
    """An "OTTO" program over the fixture's cmap, name and post tables, with
    the outline table given and, when metrics is true, 'hhea' and 'hmtx'."""
    t = {b"cmap": tables[b"cmap"], b"head": tables[b"head"], b"maxp": maxp05,
         b"name": tables[b"name"], b"post": tables[b"post"]}
    t.update(outlines)
    if metrics:
        t[b"hhea"] = tables[b"hhea"]
        t[b"hmtx"] = tables[b"hmtx"]
    return _truetype.assemble(b"OTTO", t)


cff2 = otto({b"CFF2": _cff.build_cff2()})
cff_no_metrics = otto({b"CFF ": _cff.build()}, metrics=False)
whole = otto({b"CFF ": _cff.build()})


def descriptor(tag, program, flags):
    stream = Stream(pdf, program)
    stream[Name.Subtype] = Name.OpenType
    d = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + tag),
        Flags=flags,
        FontBBox=Array([0, -200, 550, 700]),
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
    )
    d.FontFile3 = pdf.make_indirect(stream)
    return pdf.make_indirect(d)


def simple(tag, subtype, program, widths):
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name("/" + subtype),
            BaseFont=Name("/" + tag),
            FirstChar=65,
            LastChar=66,
            Widths=Array(widths),
            Encoding=Name.WinAnsiEncoding,
            FontDescriptor=descriptor(tag, program, NONSYMBOLIC),
        )
    )


def type0(tag, subtype, program, width):
    cid = Dictionary(
        Type=Name.Font,
        Subtype=Name("/" + subtype),
        BaseFont=Name("/" + tag),
        CIDSystemInfo=Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0),
        W=Array([1, Array([width, width])]),
        FontDescriptor=descriptor(tag, program, SYMBOLIC),
    )
    if subtype == "CIDFontType2":
        cid.CIDToGIDMap = Name.Identity
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type0,
            BaseFont=Name("/" + tag),
            Encoding=Name("/Identity-H"),
            DescendantFonts=Array([pdf.make_indirect(cid)]),
        )
    )


fonts = [
    simple("F1", "Type1", cff2, [600, 600]),
    type0("F2", "CIDFontType0", cff2, 600),
    simple("F3", "TrueType", cff2, [600, 600]),
    type0("F4", "CIDFontType2", cff2, 600),
    simple("F5", "TrueType", cff2, [500, 500]),
    simple("M1", "Type1", tt, [600, 600]),
    type0("M2", "CIDFontType0", tt, 600),
    simple("M3", "TrueType", cff_no_metrics, [600, 600]),
    type0("M4", "CIDFontType2", cff_no_metrics, 600),
    simple("OK", "Type1", whole, [600, 600]),
]

resources = Dictionary()
body = b""
y = 750
for tag, ref in fonts:
    resources[Name("/" + tag)] = ref
    codes = b"<00010002>" if tag in ("F2", "F4", "M2", "M4") else b"<4142>"
    body += b"BT 10 %d Td /%s 12 Tf %s Tj ET\n" % (y, tag.encode(), codes)
    y -= 20
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=resources),
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "fontcff2_2b.pdf", min_version="1.7")
