#!/usr/bin/env python3
"""Build usecmap_2u.pdf: /ToUnicode CMaps that build on other CMaps.

ISO 32000-1, 9.10.3 gives a /ToUnicode CMap the syntax of 9.7.5, so it can
build on another CMap two ways: the usecmap operator in its program names a
predefined CMap, and a /UseCMap entry in its stream dictionary (Table 120)
names a predefined CMap or an embedded CMap stream, which may name another in
turn. veraPDF merges the single-code mappings of a usecmap base into the
program, a base replacing the mappings defined before the operator and
yielding to those after it, and supplies every mapping of a /UseCMap base to
the codes the program leaves out. ISO 32000-1, 9.10.2 names the Adobe
Registry-Ordering-UCS2 CMaps as the source of a CIDFont's Unicode values, and
veraPDF reads Adobe's current resources, which map codes the 2009 copies
PDF.js ships leave out and drop others; PDF.js ships no Adobe-KR-UCS2 at all.
pdfjs issue5010.pdf is the one corpus file whose /ToUnicode builds on another
CMap, through usecmap and Adobe-Korea1-UCS2, where the two sources agree, and
no file names a base with /UseCMap, so no count reached any of this.

ISO 19005-2, 6.2.11.7.2 t1 fails a glyph whose code the /ToUnicode maps to no
value, and t2 one it maps to U+0000. Five Type 3 fonts each show codes 41, 60,
61 and 65 hexadecimal through glyphs whose names carry no Unicode value:

| Font | /ToUnicode                                                     | t1 | t2 |
| ---- | -------------------------------------------------------------- | -- | -- |
| T1   | runs `/No-Such-CMap usecmap`, a CMap nobody ships              | 4  | 0  |
| T2   | `/UseCMap /Adobe-Korea1-UCS2`, a predefined base               | 0  | 0  |
| T3   | `/UseCMap` a stream mapping 41 and 65                          | 2  | 0  |
| T4   | `/UseCMap` a stream mapping 65, whose `/UseCMap` maps 41       | 2  | 0  |
| T5   | maps 60 to U+0000, runs `/Adobe-Korea1-UCS2 usecmap`, maps 61 to U+0000 | 2 | 1 |

Adobe-Korea1-UCS2 maps CIDs 60 and 61 through bfchar and 41 and 65 through
bfrange, which the usecmap operator does not merge, so T5's base supplies 60,
replacing its U+0000, and nothing for 41 and 65, while its 61 keeps U+0000.

Six Type0 fonts then map no code themselves and build on an Adobe UCS2 CMap,
showing CIDs where Adobe's current resources and the PDF.js copies disagree.
Each is a CIDFontType2 over the three-glyph TrueType program whose
/CIDToGIDMap stream sends every CID it shows but 0 to a glyph, so no presence
or width rule answers:

| Font | Base                          | Shows                                                  | t1 |
| ---- | ----------------------------- | ------------------------------------------------------ | -- |
| C1   | `/Adobe-GB1-UCS2 usecmap`     | 0254 0255 0258 0259 5610 5617 0000 0100                | 1  |
| C2   | `/Adobe-CNS1-UCS2 usecmap`    | 3550 44DF 44E0 4A90 4A91 4A92 0000 0106                | 1  |
| C3   | `/Adobe-Japan1-UCS2 usecmap`  | 00E7 0143 0144 0F1B 0F1C 15FC 018A 018B 23BF 0000 0100 | 4  |
| C4   | `/Adobe-KR-UCS2 usecmap`      | 0000 0064 0065 006A 006B 0084 0104                     | 1  |
| C5   | `/UseCMap /Adobe-CNS1-UCS2`   | 4A90 4A91 4A92 4A93 4A94 4A95 0000 4AEB                | 1  |
| C6   | `/UseCMap /Adobe-Japan1-UCS2` | 5A12 5A13 0000 5A14                                    | 1  |

Adobe's CNS1 resource maps C2's CID 44DF to U+F538, a Private Use value, which
ISO 19005-2, 6.2.11.7.3 t1 and ISO 14289-2, 8.4.3 t1 fail with no /ActualText
around it. veraPDF reports both; litho reads a Private Use value from the
/ToUnicode program alone and not from the base it names, and reports neither
(see ../README.md, Known divergences).

Each shows CID 0 as well, the .notdef glyph, which 6.2.11.8 t1 fails once per
font. Two readings stay out. A /UseCMap chain that loops makes veraPDF raise
"Loop inside CMap" and abandon the validation. And CIDs 5A0D and 5A0E, which
Adobe's Japan1 resource maps through the last bfrange of a block it counts as
42 entries, veraPDF's bundled copy counts as 41, so its count-bound reader
drops them and fails both: an artifact of its copy, not of the rule, which C6
showing them would turn into a disagreeing cell.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

import zlib

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()
TT_PROGRAM = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])


def cmap_program(body, codespace=b"<00> <FF>"):
    """A /ToUnicode CMap program carrying body between its code space and end."""
    return (
        b"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n"
        b"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n"
        b"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n"
        b"1 begincodespacerange\n" + codespace + b"\nendcodespacerange\n" + body
        + b"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
    )


def bfchars(*pairs):
    out = b"%d beginbfchar\n" % (len(pairs) // 2)
    for code, value in zip(pairs[::2], pairs[1::2]):
        out += b"<%s> <%s>\n" % (code, value)
    return out + b"endbfchar\n"


def cmap_stream(program, use_cmap=None):
    stream = Stream(pdf, program)
    if use_cmap is not None:
        stream.UseCMap = use_cmap
    return pdf.make_indirect(stream)


TT_FILE = pdf.make_indirect(Stream(pdf, TT_PROGRAM, Length1=len(TT_PROGRAM)))
GLYPH = pdf.make_indirect(Stream(pdf, b"500 0 0 0 1000 1000 d1 0 0 1000 1000 re f\n"))


def type3(to_unicode):
    """A Type 3 font naming codes 41, 60, 61 and 65 with names of no Unicode value."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(zzq=GLYPH, zzr=GLYPH, zzs=GLYPH, zzt=GLYPH),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array(
                [65, Name("/zzq"), 96, Name("/zzr"), Name("/zzs"), 101, Name("/zzt")])),
            FirstChar=65,
            LastChar=101,
            Widths=Array([500] * 37),
            Resources=Dictionary(),
            ToUnicode=to_unicode,
        )
    )


type3_fonts = [
    type3(cmap_stream(cmap_program(b"/No-Such-CMap usecmap\n"))),
    type3(cmap_stream(cmap_program(b""), Name("/Adobe-Korea1-UCS2"))),
    type3(cmap_stream(cmap_program(b""), cmap_stream(cmap_program(bfchars(b"41", b"0041", b"65", b"0065"))))),
    type3(cmap_stream(cmap_program(b""), cmap_stream(cmap_program(bfchars(b"65", b"0065")),
                                                     cmap_stream(cmap_program(bfchars(b"41", b"0041")))))),
    type3(cmap_stream(cmap_program(bfchars(b"60", b"0000") + b"/Adobe-Korea1-UCS2 usecmap\n"
                                   + bfchars(b"61", b"0000")))),
]


def cid_font(name, cids, base, entry=False):
    """A Type0 font whose CIDToGIDMap sends every CID it shows but 0 to a glyph.

    Its /ToUnicode maps nothing itself and builds on the predefined CMap base,
    through a /UseCMap entry when entry is set and the usecmap operator
    otherwise.
    """
    gids = bytearray(2 * (max(cids) + 1))
    for cid in cids:
        if cid:
            gids[2 * cid + 1] = 1
    gid_map = Stream(pdf, zlib.compress(bytes(gids), 9))
    gid_map.Filter = Name.FlateDecode
    descendant = pdf.make_indirect(Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/" + name),
        CIDSystemInfo=Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0),
        DW=_truetype.ADVANCE_WIDTH,
        CIDToGIDMap=pdf.make_indirect(gid_map),
        FontDescriptor=pdf.make_indirect(Dictionary(
            Type=Name.FontDescriptor, FontName=Name("/" + name), Flags=32, ItalicAngle=0, Ascent=800,
            Descent=-200, CapHeight=700, StemV=80, FontBBox=Array([0, -200, 550, 700]),
            FontFile2=TT_FILE)),
    ))
    if entry:
        to_unicode = cmap_stream(cmap_program(b"", b"<0000> <FFFF>"), Name("/" + base))
    else:
        to_unicode = cmap_stream(cmap_program(b"/%s usecmap\n" % base.encode(), b"<0000> <FFFF>"))
    font = pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/" + name),
                                        Encoding=Name("/Identity-H"), DescendantFonts=Array([descendant]),
                                        ToUnicode=to_unicode))
    return font, b"<" + b"".join(b"%04X" % c for c in cids) + b">"


cid_fonts = [
    cid_font("LithoGB1", [0x0254, 0x0255, 0x0258, 0x0259, 0x5610, 0x5617, 0x0000, 0x0100],
             "Adobe-GB1-UCS2"),
    cid_font("LithoCNS1", [0x3550, 0x44DF, 0x44E0, 0x4A90, 0x4A91, 0x4A92, 0x0000, 0x0106],
             "Adobe-CNS1-UCS2"),
    cid_font("LithoJapan1", [0x00E7, 0x0143, 0x0144, 0x0F1B, 0x0F1C, 0x15FC, 0x018A, 0x018B, 0x23BF, 0x0000, 0x0100],
             "Adobe-Japan1-UCS2"),
    cid_font("LithoKR", [0x0000, 0x0064, 0x0065, 0x006A, 0x006B, 0x0084, 0x0104], "Adobe-KR-UCS2"),
    cid_font("LithoCNS1Entry", [0x4A90, 0x4A91, 0x4A92, 0x4A93, 0x4A94, 0x4A95, 0x0000, 0x4AEB], "Adobe-CNS1-UCS2", entry=True),
    cid_font("LithoJapan1Entry", [0x5A12, 0x5A13, 0x0000, 0x5A14], "Adobe-Japan1-UCS2", entry=True),
]

fonts = Dictionary()
content = b""
for index, font in enumerate(type3_fonts):
    fonts[Name("/T%d" % (index + 1))] = font
    content += b"BT /T%d 12 Tf 10 %d Td <41606165> Tj ET\n" % (index + 1, 780 - 20 * index)
for index, (font, shows) in enumerate(cid_fonts):
    fonts[Name("/C%d" % (index + 1))] = font
    content += b"BT /C%d 12 Tf 10 %d Td %s Tj ET\n" % (index + 1, 660 - 20 * index, shows)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=fonts),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "usecmap_2u.pdf")
