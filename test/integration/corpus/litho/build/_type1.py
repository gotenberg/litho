"""A minimal Type 1 font program, the form `/FontFile` holds.

ISO 32000-1, 9.9.1 and Table 126 make a `/FontFile` stream a Type 1 program in
the form the Adobe Type 1 Font Format defines: a clear-text portion, then the
eexec-encrypted portion holding the Private dictionary and the CharStrings,
then 512 zeros and `cleartomark`. `/Length1`, `/Length2` and `/Length3` count
the three. The corpus builders take no dependency beyond pikepdf, so this
module writes the bytes and the two encryptions (Type 1 Font Format, 7.1)
itself.

Every glyph is a rectangle with an advance of ADVANCE_WIDTH. The four
plaintext bytes each encryption starts with are zeros, so the program is the
same on every build. The first ciphertext byte is then 0xD9, which is not a
hexadecimal digit, so a reader takes the encrypted portion as binary.
"""

ADVANCE_WIDTH = 600

EEXEC_KEY = 55665
CHARSTRING_KEY = 4330
C1 = 52845
C2 = 22719
LEN_IV = 4


def _encrypt(plain, key):
    """The Type 1 encryption of plain under key, Type 1 Font Format, 7.1."""
    r = key
    out = bytearray()
    for p in plain:
        c = p ^ (r >> 8)
        out.append(c)
        r = ((c + r) * C1 + C2) & 0xFFFF
    return bytes(out)


def _num(value):
    """A Type 1 charstring integer, Type 1 Font Format, 6.2."""
    if -107 <= value <= 107:
        return bytes([value + 139])
    if 108 <= value <= 1131:
        v = value - 108
        return bytes([(v >> 8) + 247, v & 0xFF])
    if -1131 <= value <= -108:
        v = -value - 108
        return bytes([(v >> 8) + 251, v & 0xFF])
    raise ValueError("charstring integer out of the range this module writes")


def _charstring(height):
    """hsbw, then a rectangle from (50, 0) of 400 by height, then endchar."""
    out = _num(0) + _num(ADVANCE_WIDTH) + b"\x0d"  # hsbw
    if height:
        out += _num(50) + _num(0) + b"\x15"  # rmoveto
        out += _num(400) + b"\x06" + _num(height) + b"\x07" + _num(-400) + b"\x06"  # hlineto vlineto hlineto
        out += b"\x09"  # closepath
    return out + b"\x0e"  # endchar


def encoding_array(codes, fill_notdef=True):
    """The clear-text /Encoding of a program naming codes, a {code: name} map.

    fill_notdef writes the customary loop that names every code .notdef
    before the entries, so a code codes leaves out names .notdef. Without it
    the array keeps the null objects `array` creates.
    """
    out = b"/Encoding 256 array\n"
    if fill_notdef:
        out += b"0 1 255 {1 index exch /.notdef put} for\n"
    for code in sorted(codes):
        out += b"dup %d /%s put\n" % (code, codes[code].encode("ascii"))
    return out + b"readonly def\n"


def build(name=b"LithoT1", glyphs=("A", "B"), encoding=None, charstrings=True):
    """Return (program, length1, length2, length3) for a Type 1 font.

    glyphs names the CharStrings beside .notdef. encoding is the clear-text
    /Encoding entry, StandardEncoding when None. charstrings=False leaves the
    Private dictionary without a CharStrings dictionary, which ends the
    program with no glyph defined.
    """
    if encoding is None:
        encoding = b"/Encoding StandardEncoding def\n"
    clear = (
        b"%!PS-AdobeFont-1.0: " + name + b" 001.000\n"
        b"11 dict begin\n"
        b"/FontName /" + name + b" def\n"
        + encoding
        + b"/PaintType 0 def\n"
        b"/FontType 1 def\n"
        b"/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n"
        b"/FontBBox {0 -200 550 700} readonly def\n"
        b"currentdict end\n"
        b"currentfile eexec\n"
    )
    private = (
        b"dup /Private 8 dict dup begin\n"
        b"/RD{string currentfile exch readstring pop}executeonly def\n"
        b"/ND{noaccess def}executeonly def\n"
        b"/NP{noaccess put}executeonly def\n"
        b"/BlueValues [] def\n"
        b"/MinFeature {16 16} def\n"
        b"/password 5839 def\n"
        b"/lenIV %d def\n" % LEN_IV
    )
    # One subroutine, a bare return, since readers expect a /Subrs array.
    subr = _encrypt(b"\x00" * LEN_IV + b"\x0b", CHARSTRING_KEY)
    private += b"/Subrs 1 array\ndup 0 %d RD " % len(subr) + subr + b" NP\nND\n"
    if charstrings:
        names = (".notdef",) + tuple(glyphs)
        private += b"2 index /CharStrings %d dict dup begin\n" % len(names)
        for i, glyph in enumerate(names):
            cs = _encrypt(b"\x00" * LEN_IV + _charstring(0 if i == 0 else 400 + 100 * i), CHARSTRING_KEY)
            private += b"/%s %d RD " % (glyph.encode("ascii"), len(cs)) + cs + b" ND\n"
        private += b"end\n"
    private += b"end\nreadonly put\nnoaccess put\ndup /FontName get exch definefont pop\nmark currentfile closefile\n"
    encrypted = _encrypt(b"\x00\x00\x00\x00" + private, EEXEC_KEY)
    trailer = (b"0" * 64 + b"\n") * 8 + b"cleartomark\n"
    return clear + encrypted + trailer, len(clear), len(encrypted), len(trailer)
