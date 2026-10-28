#!/usr/bin/env python3
"""Build fontnohead_2b.pdf: TrueType programs with no 'head' table.

ISO 19005-2, 6.2.11.5 asks the widths of the font dictionary and of the
embedded program to be consistent, which takes the program's advances in
units of its em. 'head' states that em (unitsPerEm), and a program without the
table states none. veraPDF still reads such a program, and scales its 'hmtx'
advances as if the em were 2048 units. Each font shows codes 41 and 42 through
/WinAnsiEncoding, or CIDs 1 and 2 under Identity-H:

  H1  TrueType, advances 1229 with no 'head', /Widths 600: consistent at 2048,
      where litho read 1000 units and failed it.
  H2  CIDFontType2, advances 600 with no 'head', /W 293: consistent at 2048.

The converse, advances 600 under /Widths 600, fails at 2048 and passed at 1000.
It stays out of the fixture: a font of each kind leaves both tools reporting
the same width rule, for different glyphs, and the corpus gate compares rules.

Controls: HK the program H1 is cut from, its 'head' stating 2048 units, and
HO an OpenType program with CFF outlines and no 'head', whose widths a Type 1
font reads from the CFF and not at 2048.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

import _cff
import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()
NONSYMBOLIC = 32
SYMBOLIC = 4

full = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])
tables = _truetype.tables_of(full)


def with_advance(advance, upem=None, head=True):
    """The fixture's TrueType tables with every glyph advancing advance units,
    'head' stating upem when given, and no 'head' at all when head is false."""
    t = dict(tables)
    t[b"hmtx"] = b"".join(struct.pack(">Hh", advance, 50) for _ in _truetype.GLYPH_NAMES)
    hhea = bytearray(t[b"hhea"])
    struct.pack_into(">H", hhea, 10, advance)  # advanceWidthMax
    t[b"hhea"] = bytes(hhea)
    if upem is not None:
        h = bytearray(t[b"head"])
        struct.pack_into(">H", h, 18, upem)
        t[b"head"] = bytes(h)
    if not head:
        del t[b"head"]
    return _truetype.assemble(b"\x00\x01\x00\x00", t)


def descriptor(tag, key, program, flags, stream_extra=None):
    stream = Stream(pdf, program)
    for k, v in (stream_extra or {}).items():
        stream[k] = v
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
    d[Name("/" + key)] = pdf.make_indirect(stream)
    return pdf.make_indirect(d)


def simple(tag, subtype, key, program, widths, stream_extra=None):
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name("/" + subtype),
            BaseFont=Name("/" + tag),
            FirstChar=65,
            LastChar=66,
            Widths=Array(widths),
            Encoding=Name.WinAnsiEncoding,
            FontDescriptor=descriptor(tag, key, program, NONSYMBOLIC, stream_extra),
        )
    )


def type2(tag, program, width):
    cid = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/" + tag),
        CIDSystemInfo=Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0),
        CIDToGIDMap=Name.Identity,
        W=Array([1, Array([width, width])]),
        FontDescriptor=descriptor(tag, "FontFile2", program, SYMBOLIC),
    )
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type0,
            BaseFont=Name("/" + tag),
            Encoding=Name("/Identity-H"),
            DescendantFonts=Array([pdf.make_indirect(cid)]),
        )
    )


otto_tables = {
    b"CFF ": _cff.build(),
    b"cmap": tables[b"cmap"],
    b"hhea": tables[b"hhea"],
    b"hmtx": tables[b"hmtx"],
    b"maxp": struct.pack(">IH", 0x00005000, len(_truetype.GLYPH_NAMES)),
    b"name": tables[b"name"],
    b"post": tables[b"post"],
}

fonts = [
    simple("H1", "TrueType", "FontFile2", with_advance(1229, head=False), [600, 600]),
    type2("H2", with_advance(600, head=False), 293),
    simple("HK", "TrueType", "FontFile2", with_advance(1229, upem=2048), [600, 600]),
    simple("HO", "Type1", "FontFile3", _truetype.assemble(b"OTTO", otto_tables), [600, 600],
           {Name.Subtype: Name.OpenType}),
]

resources = Dictionary()
body = b""
y = 750
for tag, ref in fonts:
    resources[Name("/" + tag)] = ref
    codes = b"<00010002>" if tag == "H2" else b"<4142>"
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
save(pdf, "fontnohead_2b.pdf", min_version="1.7")
