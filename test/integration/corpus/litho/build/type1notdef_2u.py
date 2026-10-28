#!/usr/bin/env python3
"""Build type1notdef_2u.pdf: a code an embedded Type 1 font's encoding leaves
unnamed takes the name its program gives it.

ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
the code selects, and Table 114 makes the program's built-in encoding the base
of an /Encoding that names no table. A program that names a code .notdef gives
it the value U+0000, which ISO 19005-2, 6.2.11.7.2 t2 forbids; one that names
it nothing gives it no value, which t1 forbids. Each font embeds a readable
program, carries no /ToUnicode, and shows 41 and one code its encoding leaves
unnamed:

  N1  /FontFile whose vector is filled with .notdef and names 41 A, 42 B; no
      /Encoding. Shows 41 43: the program names 43 .notdef.
  N2  N1 with the vector left unfilled. Shows 41 43: the program names 43
      nothing.
  N3  /FontFile of /Encoding StandardEncoding under /Differences [65 /A] with
      no /BaseEncoding. Shows 41 80: StandardEncoding leaves 80 undefined.
  N4  /FontFile3 /Type1C on the Standard encoding, no /Encoding. Shows 41 80.
  N5  N1's program under /Encoding /StandardEncoding, a table veraPDF holds
      none of. Shows 41 43.
  N6  N4's program under /Encoding /StandardEncoding. Shows 41 80.

NC, N1's program showing 41 42 only, is the control. N1, N2, N3 and N5 also
fail 6.2.11.4.1 t2 for their second code, a glyph the program does not hold;
N4 and N6 do not, since a CFF holds .notdef; N2 fails 6.2.11.5 t1 too.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _cff
import _type1
from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()
NONSYMBOLIC = 32


def program(tag, encoding):
    data, l1, l2, l3 = _type1.build(name=tag.encode(), encoding=encoding)
    return data, {Name.Length1: l1, Name.Length2: l2, Name.Length3: l3}


def font(tag, key, data, extra, encoding, shows):
    stream = Stream(pdf, data)
    for k, v in extra.items():
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
    d = Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/" + tag),
        FirstChar=0,
        LastChar=255,
        Widths=Array([600] * 256),
        FontDescriptor=pdf.make_indirect(descriptor),
    )
    if encoding is not None:
        d[Name.Encoding] = encoding
    return tag, pdf.make_indirect(d), shows


filled = program("N1", _type1.encoding_array({65: "A", 66: "B"}))
unfilled = program("N2", _type1.encoding_array({65: "A", 66: "B"}, fill_notdef=False))
standard = program("N3", None)
cff = (_cff.build(), {Name.Subtype: Name.Type1C})
differences = Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A]))

fonts = [
    font("N1", "FontFile", *filled, None, b"AC"),
    font("N2", "FontFile", *unfilled, None, b"AC"),
    font("N3", "FontFile", *standard, differences, b"A\x80"),
    font("N4", "FontFile3", *cff, None, b"A\x80"),
    font("N5", "FontFile", *filled, Name.StandardEncoding, b"AC"),
    font("N6", "FontFile3", *cff, Name.StandardEncoding, b"A\x80"),
    font("NC", "FontFile", *filled, None, b"AB"),
]

resources = Dictionary()
body = b""
y = 750
for tag, ref, shows in fonts:
    resources[Name("/" + tag)] = ref
    body += b"BT 10 %d Td /%s 12 Tf <%s> Tj ET\n" % (y, tag.encode(), shows.hex().encode())
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
save(pdf, "type1notdef_2u.pdf", min_version="1.7")
