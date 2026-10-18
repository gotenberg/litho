"""A minimal bare CFF font program, the Type1C form of `FontFile3`.

ISO 19005-1, 6.3.5 test 2 and ISO 19005-2, 6.2.11.4.2 test 1 both compare a
font descriptor's `/CharSet` against the glyph names the embedded program
defines, so reaching them needs a program a validator can actually parse a
charset out of. `_truetype.py` does the same job for `FontFile2`; the corpus
builders take no dependency beyond pikepdf, so this module emits the bytes
directly.

The program holds three glyphs, `.notdef` plus `A` and `B`, drawn as plain
rectangles, which is enough to render and to resolve every code a fixture
shows. `A` and `B` are standard strings (SID 34 and 35 per ISO 32000-1,
Annex D), so the String INDEX stays empty and the charset names them by
number.

Every offset in the Top DICT is written in the 5-byte `29` form (ISO 32000-1,
Table 100), whose width does not depend on the value. The layout is therefore
fixed before the offsets are known and the file is assembled in one pass.
"""

import struct

FONT_NAME = b"LithoCFF"
GLYPH_NAMES = (".notdef", "A", "B")
ADVANCE_WIDTH = 600

# Standard string identifiers of "A" and "B", ISO 32000-1, Annex D.
SID_A = 34
SID_B = 35


def _index(entries):
    """One CFF INDEX. A count of zero is two bytes and nothing else."""
    if not entries:
        return struct.pack(">H", 0)
    offsets = [1]
    for e in entries:
        offsets.append(offsets[-1] + len(e))
    width = 1 if offsets[-1] < 256 else 2
    out = struct.pack(">HB", len(entries), width)
    for off in offsets:
        out += struct.pack(">B", off) if width == 1 else struct.pack(">H", off)
    return out + b"".join(entries)


def _dict_int(value):
    """A DICT operand in the fixed-width 32-bit form."""
    return b"\x1d" + struct.pack(">i", value)


def _t2_num(value):
    """A Type 2 charstring integer, ISO 32000-1 does not restate the encoding."""
    if -107 <= value <= 107:
        return struct.pack(">B", value + 139)
    if 108 <= value <= 1131:
        v = value - 108
        return struct.pack(">BB", (v >> 8) + 247, v & 0xFF)
    if -1131 <= value <= -108:
        v = -value - 108
        return struct.pack(">BB", (v >> 8) + 251, v & 0xFF)
    raise ValueError("charstring integer out of the range this module writes")


def _rect_charstring(height):
    """One glyph: a rectangle from (50, 0) to (450, height), then endchar."""
    out = _t2_num(50) + _t2_num(0) + b"\x15"  # rmoveto
    for dx, dy in ((400, 0), (0, height), (-400, 0)):
        out += _t2_num(dx) + _t2_num(dy)
    return out + b"\x05" + b"\x0e"  # rlineto, endchar


def build():
    """Return the bytes of a bare CFF holding .notdef, A and B."""
    charstrings = [b"\x0e", _rect_charstring(400), _rect_charstring(500)]

    private = _dict_int(ADVANCE_WIDTH) + b"\x14" + _dict_int(0) + b"\x15"

    # A Top DICT of fixed width: charset (15), CharStrings (17), Private (18).
    top_len = len(_dict_int(0) * 4) + 3
    header = b"\x01\x00\x04\x01"
    name_index = _index([FONT_NAME])
    top_index_len = len(_index([b"\x00" * top_len]))
    string_index = _index([])
    gsubr_index = _index([])

    charset_off = len(header) + len(name_index) + top_index_len + len(string_index) + len(gsubr_index)
    charset = b"\x00" + struct.pack(">HH", SID_A, SID_B)
    charstrings_off = charset_off + len(charset)
    charstrings_index = _index(charstrings)
    private_off = charstrings_off + len(charstrings_index)

    top = (
        _dict_int(charset_off) + b"\x0f"
        + _dict_int(charstrings_off) + b"\x11"
        + _dict_int(len(private)) + _dict_int(private_off) + b"\x12"
    )
    if len(top) != top_len:
        raise SystemExit("Top DICT width changed, which would move every offset")

    return (header + name_index + _index([top]) + string_index + gsubr_index
            + charset + charstrings_index + private)


def build_cid(cids):
    """Return the bytes of a CID-keyed CFF holding .notdef and one glyph per CID.

    The charset maps glyph i+1 to cids[i], so a CID the list leaves out has no
    glyph in the program. Every glyph advances ADVANCE_WIDTH: the one font
    DICT's Private DICT sets defaultWidthX, and no charstring states a width of
    its own. A CID-keyed font carries its Private DICT in an FDArray, chosen per
    glyph through FDSelect (Adobe Technical Note #5176, 19), and the ROS operator
    in the Top DICT is what marks it CID-keyed.
    """
    charstrings = [b"\x0e"] + [_rect_charstring(400) for _ in cids]
    private = _dict_int(ADVANCE_WIDTH) + b"\x14" + _dict_int(0) + b"\x15"

    def make_font_dict(private_off):
        return _dict_int(len(private)) + _dict_int(private_off) + b"\x12"

    def make_top(charset_off, charstrings_off, fdarray_off, fdselect_off):
        # ROS (12 30) naming standard string 0 twice with supplement 0, then
        # charset (15), CharStrings (17), FDArray (12 36) and FDSelect (12 37).
        return (
            _dict_int(0) + _dict_int(0) + _dict_int(0) + b"\x0c\x1e"
            + _dict_int(charset_off) + b"\x0f"
            + _dict_int(charstrings_off) + b"\x11"
            + _dict_int(fdarray_off) + b"\x0c\x24"
            + _dict_int(fdselect_off) + b"\x0c\x25"
        )

    header = b"\x01\x00\x04\x01"
    name_index = _index([FONT_NAME])
    top_index_len = len(_index([make_top(0, 0, 0, 0)]))
    string_index = _index([])
    gsubr_index = _index([])

    charset_off = len(header) + len(name_index) + top_index_len + len(string_index) + len(gsubr_index)
    charset = b"\x00" + b"".join(struct.pack(">H", c) for c in cids)
    fdselect_off = charset_off + len(charset)
    # FDSelect format 3: one range putting every glyph in font DICT 0.
    fdselect = struct.pack(">BHHBH", 3, 1, 0, 0, len(charstrings))
    charstrings_off = fdselect_off + len(fdselect)
    charstrings_index = _index(charstrings)
    fdarray_off = charstrings_off + len(charstrings_index)
    private_off = fdarray_off + len(_index([make_font_dict(0)]))

    top = make_top(charset_off, charstrings_off, fdarray_off, fdselect_off)
    return (header + name_index + _index([top]) + string_index + gsubr_index
            + charset + fdselect + charstrings_index + _index([make_font_dict(private_off)]) + private)


def build_charstrings(charstrings, default_width=ADVANCE_WIDTH):
    """build's name-keyed program with the CharStrings INDEX given.

    The charset still names A and B, so a program given fewer than three
    charstrings, or none, keeps names for glyphs it holds no program for.
    default_width is the Private DICT's defaultWidthX.
    """
    private = _dict_int(default_width) + b"\x14" + _dict_int(0) + b"\x15"
    top_len = len(_dict_int(0) * 4) + 3
    header = b"\x01\x00\x04\x01"
    name_index = _index([FONT_NAME])
    charset_off = len(header) + len(name_index) + len(_index([b"\x00" * top_len])) + 4
    charset = b"\x00" + struct.pack(">HH", SID_A, SID_B)
    charstrings_off = charset_off + len(charset)
    charstrings_index = _index(list(charstrings))
    private_off = charstrings_off + len(charstrings_index)
    top = (
        _dict_int(charset_off) + b"\x0f"
        + _dict_int(charstrings_off) + b"\x11"
        + _dict_int(len(private)) + _dict_int(private_off) + b"\x12"
    )
    if len(top) != top_len:
        raise SystemExit("Top DICT width changed, which would move every offset")
    return (header + name_index + _index([top]) + _index([]) + _index([])
            + charset + charstrings_index + private)


def build_cid_charstrings(count, default_width=ADVANCE_WIDTH):
    """A CID-keyed program whose charset maps glyphs 1 and 2 to CIDs 1 and 2,
    holding only the first count charstrings (.notdef, then one per CID).
    default_width is the one font DICT's defaultWidthX."""
    cids = [1, 2]
    charstrings = ([b"\x0e"] + [_rect_charstring(400) for _ in cids])[:count]
    private = _dict_int(default_width) + b"\x14" + _dict_int(0) + b"\x15"

    def make_top(charset_off, charstrings_off, fdarray_off, fdselect_off):
        return (
            _dict_int(0) + _dict_int(0) + _dict_int(0) + b"\x0c\x1e"
            + _dict_int(charset_off) + b"\x0f"
            + _dict_int(charstrings_off) + b"\x11"
            + _dict_int(fdarray_off) + b"\x0c\x24"
            + _dict_int(fdselect_off) + b"\x0c\x25"
        )

    header = b"\x01\x00\x04\x01"
    name_index = _index([FONT_NAME])
    charset_off = len(header) + len(name_index) + len(_index([make_top(0, 0, 0, 0)])) + 4
    charset = b"\x00" + b"".join(struct.pack(">H", c) for c in cids)
    fdselect_off = charset_off + len(charset)
    fdselect = struct.pack(">BHHBH", 3, 1, 0, 0, max(count, 1))
    charstrings_off = fdselect_off + len(fdselect)
    charstrings_index = _index(charstrings)
    fdarray_off = charstrings_off + len(charstrings_index)
    font_dict_len = len(_dict_int(0)) * 2 + 1
    private_off = fdarray_off + len(_index([b"\x00" * font_dict_len]))
    font_dict = _dict_int(len(private)) + _dict_int(private_off) + b"\x12"
    top = make_top(charset_off, charstrings_off, fdarray_off, fdselect_off)
    return (header + name_index + _index([top]) + _index([]) + _index([])
            + charset + fdselect + charstrings_index + _index([font_dict]) + private)


def _cff2_index(entries):
    """A CFF2 INDEX, whose count is a Card32 (OpenType CFF2 table, INDEX Data)."""
    if not entries:
        return struct.pack(">I", 0)
    offsets = [1]
    for e in entries:
        offsets.append(offsets[-1] + len(e))
    out = struct.pack(">IB", len(entries), 2)
    for off in offsets:
        out += struct.pack(">H", off)
    return out + b"".join(entries)


def _cff2_rect(height):
    """A CFF2 charstring: the rectangle of _rect_charstring with no width
    operand and no endchar, neither of which CFF2 has."""
    out = _t2_num(50) + _t2_num(0) + b"\x15"
    for dx, dy in ((400, 0), (0, height), (-400, 0)):
        out += _t2_num(dx) + _t2_num(dy)
    return out + b"\x05"


def build_cff2():
    """Return a 'CFF2' table holding .notdef, A and B (OpenType, CFF2 table).

    CFF2 drops the Name, String and charset structures and the Top DICT INDEX:
    a 5-byte header gives the length of a bare Top DICT, which names the
    CharStrings INDEX and the FDArray holding the one Private DICT. Advance
    widths live in 'hmtx' alone.
    """
    charstrings = [_cff2_rect(100), _cff2_rect(700), _cff2_rect(500)]
    private = _dict_int(0) + b"\x0a"  # StdHW 0, so the Private DICT is not empty
    top_len = len(_dict_int(0)) * 2 + 1 + 2
    gsubrs = _cff2_index([])
    charstrings_off = 5 + top_len + len(gsubrs)
    charstrings_index = _cff2_index(charstrings)
    fdarray_off = charstrings_off + len(charstrings_index)
    font_dict_len = len(_dict_int(0)) * 2 + 1
    private_off = fdarray_off + len(_cff2_index([b"\x00" * font_dict_len]))
    font_dict = _dict_int(len(private)) + _dict_int(private_off) + b"\x12"
    top = _dict_int(charstrings_off) + b"\x11" + _dict_int(fdarray_off) + b"\x0c\x24"
    if len(top) != top_len:
        raise SystemExit("Top DICT width changed, which would move every offset")
    return (struct.pack(">BBBH", 2, 0, 5, top_len) + top + gsubrs + charstrings_index
            + _cff2_index([font_dict]) + private)
