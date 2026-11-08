#!/usr/bin/env python3
"""Build fontotread_2b.pdf: glyph rules read an OpenType program by its font.

veraPDF reads an embedded sfnt by the font that embeds it, not by the
outlines it carries, for every rule, not only for the embedding verdict that
fontcff2_2b.pdf plants. A Type 1 or CIDFontType0 font reads its 'CFF ' table
and nothing else of the sfnt; a TrueType or CIDFontType2 font reads any sfnt,
"OTTO" included, as a TrueType program through 'hmtx', 'maxp' and 'head'.
Every program here is a /FontFile3 /OpenType. Its CFF charstrings advance 600,
and its 'hmtx' states 500, so the two readings disagree on every width. Each
simple font shows codes 41 and 42 through /WinAnsiEncoding, each Type 0 font
CIDs 1 and 2 under Identity-H:

  W1  Type 1 over an "OTTO", /Widths 600, as the 'CFF ' table states.
  W2  Type 1 over the same program, /Widths 500, as 'hmtx' states.
  W3  Type 1 over a version-1.0 sfnt carrying 'glyf' and a 'CFF ' table,
      /Widths 600.
  W4  CIDFontType0 over a version-1.0 sfnt carrying 'glyf' and a CID-keyed
      'CFF ' table, /W 600.
  W5  CIDFontType2 over an "OTTO" with a CID-keyed 'CFF ' table, /W 500, as
      'hmtx' states.
  W6  CIDFontType2 over the same program, /W 600.
  H1  TrueType over an "OTTO" with no 'head', whose 'hmtx' advances 1000:
      a TrueType program with no 'head' weighs its advances against 2048
      units, so /Widths 488.
  P1  CIDFontType2 over an "OTTO" whose 'maxp' counts three glyphs and whose
      'CFF ' charset holds CIDs 1 to 3, showing CIDs 1 and 3.

W2 and W6 fail the width rule, W1, W3, W4, W5 and H1 pass it.

See ../FIXTURES.md for the authoritative expected firing set.
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
glyphs = len(_truetype.GLYPH_NAMES)
maxp05 = struct.pack(">IH", 0x00005000, glyphs)


def hmtx(advance):
    return b"".join(struct.pack(">Hh", advance, 50) for _ in range(glyphs))


def sfnt(version, cff, advance=500, head=True, glyf=False):
    """An sfnt under version over the fixture's cmap, hhea, name and post
    tables, with the 'CFF ' table given and 'hmtx' advancing advance. glyf
    adds the TrueType outlines and a version-1.0 'maxp'."""
    t = {b"cmap": tables[b"cmap"], b"hhea": tables[b"hhea"], b"hmtx": hmtx(advance),
         b"name": tables[b"name"], b"post": tables[b"post"], b"CFF ": cff}
    if head:
        t[b"head"] = tables[b"head"]
    if glyf:
        t[b"glyf"] = tables[b"glyf"]
        t[b"loca"] = tables[b"loca"]
        t[b"maxp"] = tables[b"maxp"]
    else:
        t[b"maxp"] = maxp05
    return _truetype.assemble(version, t)


name_keyed = sfnt(b"OTTO", _cff.build())
name_keyed_tt = sfnt(b"\x00\x01\x00\x00", _cff.build(), glyf=True)
cid_keyed = sfnt(b"OTTO", _cff.build_cid([1, 2]))
cid_keyed_tt = sfnt(b"\x00\x01\x00\x00", _cff.build_cid([1, 2]), glyf=True)
headless = sfnt(b"OTTO", _cff.build(), advance=1000, head=False)
three_cids = sfnt(b"OTTO", _cff.build_cid([1, 2, 3]))


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


def simple(tag, subtype, program, width):
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name("/" + subtype),
            BaseFont=Name("/" + tag),
            FirstChar=65,
            LastChar=66,
            Widths=Array([width, width]),
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
        W=Array([1, Array([width, width, width])]),
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
    simple("W1", "Type1", name_keyed, 600),
    simple("W2", "Type1", name_keyed, 500),
    simple("W3", "Type1", name_keyed_tt, 600),
    type0("W4", "CIDFontType0", cid_keyed_tt, 600),
    type0("W5", "CIDFontType2", cid_keyed, 500),
    type0("W6", "CIDFontType2", cid_keyed, 600),
    simple("H1", "TrueType", headless, 488),
    type0("P1", "CIDFontType2", three_cids, 500),
]
shown = {"W4": b"<00010002>", "W5": b"<00010002>", "W6": b"<00010002>", "P1": b"<00010003>"}

resources = Dictionary()
body = b""
y = 750
for tag, ref in fonts:
    resources[Name("/" + tag)] = ref
    body += b"BT 10 %d Td /%s 12 Tf %s Tj ET\n" % (y, tag.encode(), shown.get(tag, b"<4142>"))
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
save(pdf, "fontotread_2b.pdf", min_version="1.7")
