#!/usr/bin/env python3
"""Build fontunread_2b.pdf: font programs in the right container that a
validator's parser cannot finish embed nothing.

ISO 19005-2, 6.2.11.4.1 t1 asks that every font program be embedded (ISO
32000-1, 9.9). veraPDF answers it with the program's own parse: a font whose
program does not parse is reported as not embedded, and no rule weighs its
glyphs, widths or cmap. Each font shows codes 41 and 42 through
/WinAnsiEncoding:

  U1  Type 1, /FontFile holding the clear-text portion alone, no eexec.
  U2  Type 1, /FontFile whose Private dictionary defines no /CharStrings.
  U3  Type 1, /FontFile3 /Type1C whose Top DICT INDEX is empty.
  U4  Type 1, /FontFile3 /Type1C whose Top DICT states no CharStrings.
  T1  TrueType, /FontFile2 an sfnt with no table at all.
  T2  TrueType, /FontFile2 with every table but 'hmtx'.
  T3  TrueType, /FontFile2 with every table but 'hhea'.
  O1  Type 1, /FontFile3 /OpenType an "OTTO" with no table.

Controls, each embedded: OK the well-formed Type 1 program U1 and U2 are cut
from, CN a CFF with an empty Name INDEX, and TK the well-formed TrueType T2
and T3 are cut from.

See ../README.md for the authoritative expected firing set.
"""

import struct

import _cff
import _truetype
import _type1
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()
NONSYMBOLIC = 32


def drop_tables(font, drop):
    """Rewrite an sfnt without the tables named in drop."""
    num = struct.unpack(">H", font[4:6])[0]
    tables = {}
    for i in range(num):
        tag, _, off, length = struct.unpack(">4sIII", font[12 + 16 * i: 28 + 16 * i])
        tables[tag] = font[off: off + length]
    tags = [t for t in sorted(tables) if t not in drop]
    offset = 12 + 16 * len(tags)
    records = b""
    body = b""
    for tag in tags:
        data = tables[tag]
        records += struct.pack(">4sIII", tag, _truetype._checksum(data), offset, len(data))
        padded = data + b"\0" * (-len(data) % 4)
        body += padded
        offset += len(padded)
    return font[:4] + struct.pack(">HHHH", len(tags), 16, 0, 0) + records + body


def cff_program(names=(_cff.FONT_NAME,), charstrings_op=True):
    """_cff.build's program, with the Name INDEX given and the CharStrings
    operator replaced by a no-op when charstrings_op is false."""
    charstrings = [b"\x0e", _cff._rect_charstring(400), _cff._rect_charstring(500)]
    private = _cff._dict_int(_cff.ADVANCE_WIDTH) + b"\x14" + _cff._dict_int(0) + b"\x15"
    top_len = len(_cff._dict_int(0) * 4) + 3
    header = b"\x01\x00\x04\x01"
    name_index = _cff._index(list(names))
    head_len = len(header) + len(name_index) + len(_cff._index([b"\x00" * top_len])) + 4
    charset_off = head_len
    charset = b"\x00" + struct.pack(">HH", _cff.SID_A, _cff.SID_B)
    charstrings_off = charset_off + len(charset)
    charstrings_index = _cff._index(charstrings)
    private_off = charstrings_off + len(charstrings_index)
    # Operator 13 is UniqueID, which a reader skips; 17 is CharStrings.
    cs = _cff._dict_int(charstrings_off) + (b"\x11" if charstrings_op else b"\x0d")
    top = (_cff._dict_int(charset_off) + b"\x0f" + cs
           + _cff._dict_int(len(private)) + _cff._dict_int(private_off) + b"\x12")
    assert len(top) == top_len
    return (header + name_index + _cff._index([top]) + _cff._index([]) + _cff._index([])
            + charset + charstrings_index + private)


def font(tag, subtype, key, program, stream_extra=None):
    stream = Stream(pdf, program)
    for k, v in (stream_extra or {}).items():
        stream[k] = v
    descriptor = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + tag),
        Flags=NONSYMBOLIC,
        FontBBox=Array([0, -200, 550, 700]),
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
    )
    descriptor[Name("/" + key)] = pdf.make_indirect(stream)
    return tag, pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name("/" + subtype),
            BaseFont=Name("/" + tag),
            FirstChar=65,
            LastChar=66,
            Widths=Array([600, 600]),
            Encoding=Name.WinAnsiEncoding,
            FontDescriptor=pdf.make_indirect(descriptor),
        )
    )


def type1(tag, charstrings=True):
    program, l1, l2, l3 = _type1.build(name=tag.encode(), encoding=_type1.encoding_array({65: "A", 66: "B"}),
                                       charstrings=charstrings)
    return program, {Name.Length1: l1, Name.Length2: l2, Name.Length3: l3}


ok_program, ok_lengths = type1("OK")
clear = ok_program[: ok_lengths[Name.Length1]].replace(b"currentfile eexec\n", b"")
truetype = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])
TYPE1C = {Name.Subtype: Name.Type1C}

fonts = [
    font("U1", "Type1", "FontFile", clear, {Name.Length1: len(clear), Name.Length2: 0, Name.Length3: 0}),
    font("U2", "Type1", "FontFile", *type1("U2", charstrings=False)),
    font("U3", "Type1", "FontFile3", _cff.build()[:4] + _cff._index([_cff.FONT_NAME]) + _cff._index([]) * 3, TYPE1C),
    font("U4", "Type1", "FontFile3", cff_program(charstrings_op=False), TYPE1C),
    font("T1", "TrueType", "FontFile2", truetype[:4] + struct.pack(">HHHH", 0, 0, 0, 0)),
    font("T2", "TrueType", "FontFile2", drop_tables(truetype, {b"hmtx"})),
    font("T3", "TrueType", "FontFile2", drop_tables(truetype, {b"hhea"})),
    font("O1", "Type1", "FontFile3", b"OTTO" + struct.pack(">HHHH", 0, 0, 0, 0), {Name.Subtype: Name.OpenType}),
    font("OK", "Type1", "FontFile", ok_program, ok_lengths),
    font("CN", "Type1", "FontFile3", cff_program(names=()), TYPE1C),
    font("TK", "TrueType", "FontFile2", truetype),
]

resources = Dictionary()
body = b""
y = 750
for tag, ref in fonts:
    resources[Name("/" + tag)] = ref
    body += b"BT 10 %d Td /%s 12 Tf <4142> Tj ET\n" % (y, tag.encode())
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
save(pdf, "fontunread_2b.pdf", min_version="1.7")
