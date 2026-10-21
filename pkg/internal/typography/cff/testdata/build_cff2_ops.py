"""Write cff2_ops.cff2, a bare 'CFF2' table crafted to run the CFF2 reader.

The bytes are encoded here by hand, with no font library, so fontTools reads
a program it did not write (gen_outlines_golden.py records that reading). The
table holds (OpenType specification, CFF2 table):

  - a Top DICT with a FontMatrix of 2000 units per em, a VariationStore, an
    FDArray of two Font DICTs and an FDSelect of format 3;
  - a VariationStore of two ItemVariationData: index 0 selects two regions
    (k = 2), index 1 one region (k = 1);
  - a Private DICT per Font DICT: the first blends StdHW, the second sets
    vsindex 1; each holds one local subroutine that blends;
  - one global subroutine;
  - seven glyphs: a plain rectangle, blends of n = 2 and stacked blends of
    n = 1, calls to local and global subroutines, stems, hintmask over an
    implicit vstemhm and cntrmask, every curve operator and the flex family,
    a Font DICT whose Private DICT selects k = 1, a charstring vsindex, an
    empty charstring, and 121 operands at once, past the 48 of Type 2.

Run it from anywhere: python3 build_cff2_ops.py. It needs no dependency.
"""

import os
import struct

HERE = os.path.dirname(os.path.abspath(__file__))

# Charstring operators (OpenType CFF2 table, CharString operator summary).
OPS = {
    "hstem": b"\x01", "vstem": b"\x03", "vmoveto": b"\x04", "rlineto": b"\x05",
    "hlineto": b"\x06", "vlineto": b"\x07", "rrcurveto": b"\x08",
    "callsubr": b"\x0a", "vsindex": b"\x0f", "blend": b"\x10",
    "hstemhm": b"\x12", "hintmask": b"\x13", "cntrmask": b"\x14",
    "rmoveto": b"\x15", "hmoveto": b"\x16", "vstemhm": b"\x17",
    "rcurveline": b"\x18", "rlinecurve": b"\x19", "vvcurveto": b"\x1a",
    "hhcurveto": b"\x1b", "callgsubr": b"\x1d", "vhcurveto": b"\x1e",
    "hvcurveto": b"\x1f", "hflex": b"\x0c\x22", "flex": b"\x0c\x23",
    "hflex1": b"\x0c\x24", "flex1": b"\x0c\x25",
}


def num(value):
    """One charstring number, in the shortest of the Type 2 integer forms."""
    if -107 <= value <= 107:
        return struct.pack(">B", value + 139)
    if 108 <= value <= 1131:
        v = value - 108
        return struct.pack(">BB", (v >> 8) + 247, v & 0xFF)
    if -1131 <= value <= -108:
        v = -value - 108
        return struct.pack(">BB", (v >> 8) + 251, v & 0xFF)
    return b"\x1c" + struct.pack(">h", value)


def cs(*tokens):
    """A charstring from numbers, operator names and raw mask bytes."""
    out = b""
    for t in tokens:
        if isinstance(t, bytes):
            out += t
        elif isinstance(t, str):
            out += OPS[t]
        else:
            out += num(t)
    return out


def dict_int(value):
    """A DICT integer in the fixed-width 5-byte form."""
    return b"\x1d" + struct.pack(">i", value)


def index(entries):
    """A CFF2 INDEX: a Card32 count, then offsets of 2 bytes."""
    if not entries:
        return struct.pack(">I", 0)
    offsets = [1]
    for e in entries:
        offsets.append(offsets[-1] + len(e))
    return struct.pack(">IB", len(entries), 2) + b"".join(struct.pack(">H", o) for o in offsets) + b"".join(entries)


def variation_store():
    """A VariationStore: one axis, two regions, and two ItemVariationData,
    the first selecting regions 0 and 1, the second region 0."""
    regions = struct.pack(">HH", 1, 2) + struct.pack(">hhh", 0, 0x4000, 0x4000) + struct.pack(">hhh", -0x4000, -0x4000, 0)
    ivd0 = struct.pack(">HHH", 0, 0, 2) + struct.pack(">HH", 0, 1)
    ivd1 = struct.pack(">HHH", 0, 0, 1) + struct.pack(">H", 0)
    head = 2 + 4 + 2 + 4 * 2
    region_off = head
    ivd0_off = region_off + len(regions)
    ivd1_off = ivd0_off + len(ivd0)
    store = struct.pack(">HIHII", 1, region_off, 2, ivd0_off, ivd1_off) + regions + ivd0 + ivd1
    return struct.pack(">H", len(store)) + store


GLOBAL_SUBRS = [cs(-300, 0, "rlineto")]

LOCAL_SUBRS = [
    # Font DICT 0, k = 2.
    [cs(0, 400, 0, 0, 30, 40, 2, "blend", "rlineto")],
    # Font DICT 1, k = 1.
    [cs(0, -250, 0, 25, 2, "blend", "rlineto")],
]

GLYPHS = [
    # .notdef: a rectangle, no blend.
    cs(0, 0, "rmoveto", 500, 700, -500, "hlineto"),
    # Blends of n = 2 (k = 2), a local and a global subroutine, then two
    # stacked blends of n = 1 before one rlineto.
    cs(100, 50, 10, 20, 5, 6, 2, "blend", "rmoveto", 300, "hlineto",
       -107, "callsubr", -107, "callgsubr",
       50, -100, "rmoveto", 10, 1, 2, 1, "blend", 20, 3, 4, 1, "blend", "rlineto"),
    # Stems, hintmask over an implicit vstemhm, cntrmask, then every curve
    # operator and the flex family.
    cs(10, 20, 30, 40, "hstemhm", 50, 60, "hintmask", b"\xe0", "cntrmask", b"\xa0",
       10, 10, "rmoveto",
       10, 20, 30, 40, 50, 60, "rrcurveto",
       5, 10, 15, 20, 25, 30, 35, 40, "rcurveline",
       10, 0, 0, 10, 5, 10, 15, 20, 25, 30, "rlinecurve",
       7, 10, 20, 30, 40, "vvcurveto",
       9, 10, 20, 30, 40, 50, 60, 70, 80, "hhcurveto",
       10, 20, 30, 40, 50, 60, 70, 80, 90, "vhcurveto",
       15, 25, 35, 45, 55, 65, 75, 85, "hvcurveto",
       20, 30, 40, 50, 60, 70, 80, "hflex",
       10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 50, "flex",
       10, 20, 30, 40, 50, 60, 70, 80, 90, "hflex1",
       10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, "flex1"),
    # Font DICT 1, whose Private DICT sets vsindex 1 (k = 1): a blend of
    # n = 2 and its local subroutine, which blends with k = 1 too.
    cs(200, 0, 15, 0, 2, "blend", "rmoveto", 0, 300, "rlineto", -107, "callsubr", -200, "hlineto"),
    # Font DICT 1 again, a charstring vsindex 0 selecting k = 2.
    cs(0, "vsindex", 40, 60, 1, 2, 3, 4, 2, "blend", "rmoveto", 100, 10, 20, 1, "blend", "hlineto",
       100, "vlineto", -100, "hlineto"),
    # An empty charstring: CFF2 has no endchar.
    b"",
    # 121 operands at once: 40 defaults, 80 deltas and n.
    cs(0, 0, "rmoveto", *([10, 5] * 20), *([1, 2] * 40), 40, "blend", "rlineto"),
]

# FDSelect format 3: glyphs 0-2 in Font DICT 0, 3-4 in 1, 5-6 in 0.
FD_RANGES = [(0, 0), (3, 1), (5, 0)]

# FontMatrix 0.0005 0 0 0.0005 0 0 in real-number form: "5E-4" is the nibbles
# 5 c 4 f, and 0 is the nibbles 0 f.
REAL_5EM4 = b"\x1e\x5c\x4f"
REAL_0 = b"\x1e\x0f"
FONT_MATRIX = REAL_5EM4 + REAL_0 + REAL_0 + REAL_5EM4 + REAL_0 + REAL_0 + b"\x0c\x07"


def private_dict(fd):
    """Private DICT of Font DICT fd. The Subrs offset (19) is relative to the
    Private DICT and points right past it."""
    if fd == 0:
        # StdHW 55, blended with deltas -29 19 (k = 2): 55 -29 19 1 blend StdHW.
        body = num(55) + num(-29) + num(19) + num(1) + b"\x17" + b"\x0a"
    else:
        body = dict_int(1) + b"\x16"
    body_len = len(body) + 5 + 1
    return body + dict_int(body_len) + b"\x13"


def build():
    store = variation_store()
    gsubrs = index(GLOBAL_SUBRS)
    charstrings = index(GLYPHS)
    sentinel = len(GLYPHS)
    fdselect = struct.pack(">BH", 3, len(FD_RANGES)) + b"".join(struct.pack(">HB", g, f) for g, f in FD_RANGES) + struct.pack(">H", sentinel)
    privates = [private_dict(fd) for fd in range(2)]
    locals_ = [index(s) for s in LOCAL_SUBRS]

    def top(cs_off, fdarray_off, fdselect_off, vstore_off):
        return (FONT_MATRIX + dict_int(cs_off) + b"\x11" + dict_int(vstore_off) + b"\x18"
                + dict_int(fdarray_off) + b"\x0c\x24" + dict_int(fdselect_off) + b"\x0c\x25")

    top_len = len(top(0, 0, 0, 0))
    font_dict_len = len(dict_int(0)) * 2 + 1
    fdarray_len = len(index([b"\x00" * font_dict_len] * 2))

    pos = 5 + top_len + len(gsubrs)
    vstore_off = pos
    pos += len(store)
    cs_off = pos
    pos += len(charstrings)
    fdselect_off = pos
    pos += len(fdselect)
    fdarray_off = pos
    pos += fdarray_len
    blocks = b""
    font_dicts = []
    for fd in range(2):
        font_dicts.append(dict_int(len(privates[fd])) + dict_int(pos) + b"\x12")
        blocks += privates[fd] + locals_[fd]
        pos += len(privates[fd]) + len(locals_[fd])
    fdarray = index(font_dicts)
    if len(fdarray) != fdarray_len:
        raise SystemExit("FDArray width changed, which would move every offset")
    t = top(cs_off, fdarray_off, fdselect_off, vstore_off)
    return struct.pack(">BBBH", 2, 0, 5, len(t)) + t + gsubrs + store + charstrings + fdselect + fdarray + blocks


def main():
    with open(os.path.join(HERE, "cff2_ops.cff2"), "wb") as fh:
        fh.write(build())


if __name__ == "__main__":
    main()
