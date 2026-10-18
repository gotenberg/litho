#!/usr/bin/env python3
"""Build cmapcidtext_2b.pdf: text read through the CID an encoding CMap gives.

A Type 0 font's encoding CMap takes a character code to a CID (ISO 32000-1,
9.7.6.2), and the tables a reader falls back on when the font has no
/ToUnicode CMap are keyed by CID: the character collection's UCS2 CMap
(9.10.2), and the /CIDToGIDMap stream with the embedded program's cmap behind
it. Both fonts here encode one-byte codes their CMap sends to CIDs other than
the codes, so a reader that took the code as the CID reads other characters.
No corpus file shows text this way without a /ToUnicode CMap that answers
first.

| Font | Encoding CMap        | Shows | CIDs  | Route                                   | Text |
| ---- | -------------------- | ----- | ----- | --------------------------------------- | ---- |
| G1   | <61> to 2, <62> to 1 | 61 62 | 2, 1  | /CIDToGIDMap 1 to A, 2 to B, then cmap  | BA   |
| G2   | <61> to 35, <62> to 34 | 61 62 | 35, 34 | Adobe-Japan1 UCS2 (CID 34 is U+0041) | BA   |

G1 embeds the three-glyph program of _truetype, so both its CIDs select a
glyph. G2 is a reference-only Adobe-Japan1 font: it fails the embedding
rules, and is there because ISO 32000-1, 9.10.2 reads its CIDs through the
collection whether or not a program is embedded.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set, and _encodingcmap for the font program.
"""

import zlib

import _truetype
from _common import Array, Dictionary, Name, Stream
from _encodingcmap import Fixture, system_info

ONE_BYTE = b"<00> <FF>"

f = Fixture()

gid_map = Stream(f.pdf, zlib.compress(bytes([0, 0, 0, 1, 0, 2]), 9))
gid_map.Filter = Name.FlateDecode
f.font("G1", "LithoCIDText", f.cmap("LithoG1", b"2 begincidchar\n<61> 2\n<62> 1\nendcidchar\n", codespace=ONE_BYTE),
       b"6162", gid_map=f.pdf.make_indirect(gid_map))

japan1 = system_info("Japan1", 6)
descendant = f.pdf.make_indirect(Dictionary(
    Type=Name.Font,
    Subtype=Name.CIDFontType2,
    BaseFont=Name("/LithoCIDTextJapan"),
    CIDSystemInfo=japan1,
    DW=_truetype.ADVANCE_WIDTH,
    CIDToGIDMap=Name.Identity,
    FontDescriptor=f.pdf.make_indirect(Dictionary(
        Type=Name.FontDescriptor, FontName=Name("/LithoCIDTextJapan"), Flags=32, ItalicAngle=0, Ascent=800,
        Descent=-200, CapHeight=700, StemV=80, FontBBox=Array([0, -200, 550, 700]))),
))
encoding = f.cmap("LithoG2", b"2 begincidchar\n<61> 35\n<62> 34\nendcidchar\n", info=japan1, codespace=ONE_BYTE)
f.fonts.append(("G2", f.pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/LithoCIDTextJapan"), Encoding=encoding,
    DescendantFonts=Array([descendant]))), b"6162"))

f.save("cmapcidtext_2b.pdf")
