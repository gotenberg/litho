#!/usr/bin/env python3
"""Build fontemptycs_2b.pdf: CFF programs whose CharStrings INDEX is empty.

Such a program still reads: its header leads to a Top DICT and on to a
CharStrings INDEX, which holds no glyph. ISO 19005-2, 6.2.11.4.1 t1 then counts
the font as embedded, 6.2.11.4.1 t2 finds no glyph for any code it shows, and
6.2.11.5 still weighs the width each glyph is drawn with against the one the
font dictionary declares. veraPDF takes that width from the font: the
defaultWidthX of the Private DICT, or, for a subset or a CID-keyed program,
the width the font dictionary gives a missing glyph. Each simple font shows
codes 41 and 42 through /WinAnsiEncoding, each Type 0 font CIDs 1 and 2 under
Identity-H:

  E1  Type 1, defaultWidthX 700, /Widths 700.
  E2  Type 1, defaultWidthX 700, /Widths 0.
  E3  Type 1, a subset (ABCDEF+E3), defaultWidthX 700, /Widths 700, no
      /MissingWidth.
  E4  Type 1, a subset (ABCDEF+E4), /Widths 300, /MissingWidth 300.
  E5  Type 1, defaultWidthX 700, /Widths 700, a /Differences naming code 41
      .notdef.
  C1  CIDFontType0, CID-keyed, /W 1000 for both CIDs, no /DW.
  C2  CIDFontType0, CID-keyed, /W 500.
  K1  CIDFontType0, name-keyed, defaultWidthX 700, /W 700.
  K2  CIDFontType0, name-keyed, defaultWidthX 700, /W 0.

Controls: EK the whole program E1 to E5 are cut from, and CK the whole
CID-keyed program C1 and C2 are cut from.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _cff
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()
NONSYMBOLIC = 32
SYMBOLIC = 4
DEFAULT_WIDTH = 700

empty = _cff.build_charstrings([], DEFAULT_WIDTH)


def descriptor(tag, program, subtype, flags, extra=None):
    stream = Stream(pdf, program)
    stream[Name.Subtype] = Name("/" + subtype)
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
    for k, v in (extra or {}).items():
        d[k] = v
    d.FontFile3 = pdf.make_indirect(stream)
    return pdf.make_indirect(d)


def simple(tag, program, widths, base=None, encoding=None, extra=None):
    name = base or tag
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type1,
            BaseFont=Name("/" + name),
            FirstChar=65,
            LastChar=66,
            Widths=Array(widths),
            Encoding=encoding if encoding is not None else Name.WinAnsiEncoding,
            FontDescriptor=descriptor(name, program, "Type1C", NONSYMBOLIC, extra),
        )
    )


def type0(tag, program, width):
    cid = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType0,
        BaseFont=Name("/" + tag),
        CIDSystemInfo=Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0),
        W=Array([1, Array([width, width])]),
        FontDescriptor=descriptor(tag, program, "CIDFontType0C", SYMBOLIC),
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


notdef_41 = Dictionary(Type=Name.Encoding, BaseEncoding=Name.WinAnsiEncoding,
                       Differences=Array([65, Name("/.notdef")]))

fonts = [
    simple("E1", empty, [700, 700]),
    simple("E2", empty, [0, 0]),
    simple("E3", empty, [700, 700], base="ABCDEF+E3"),
    simple("E4", empty, [300, 300], base="ABCDEF+E4", extra={Name.MissingWidth: 300}),
    simple("E5", empty, [700, 700], encoding=notdef_41),
    type0("C1", _cff.build_cid_charstrings(0, DEFAULT_WIDTH), 1000),
    type0("C2", _cff.build_cid_charstrings(0, DEFAULT_WIDTH), 500),
    type0("K1", empty, 700),
    type0("K2", empty, 0),
    simple("EK", _cff.build(), [600, 600]),
    type0("CK", _cff.build_cid_charstrings(3, _cff.ADVANCE_WIDTH), 600),
]

resources = Dictionary()
body = b""
y = 750
for tag, ref in fonts:
    resources[Name("/" + tag)] = ref
    codes = b"<00010002>" if tag[0] in "CK" else b"<4142>"
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
save(pdf, "fontemptycs_2b.pdf", min_version="1.7")
