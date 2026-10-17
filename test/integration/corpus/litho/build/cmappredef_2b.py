#!/usr/bin/env python3
"""Build cmappredef_2b.pdf: Type 0 fonts whose /Encoding names a predefined CMap.

A predefined CMap can build on another through usecmap, and declare notdef
ranges (ISO 32000-1, 9.7.5.2 and 9.7.6.3). veraPDF's PDCMap parses the bundled
CMap a name denotes with the parser it runs on an embedded stream, so it
follows the chain and applies the ranges, and sends a code nothing maps to CID
0. Every -V CMap holds only its vertical forms and takes the rest, its code
space included, from its -H base. No corpus file embeds the program of a font
whose /Encoding names a predefined CMap and shows a code that reaches a base,
a notdef range or no mapping, so no count reached this.

Every font is a CIDFontType2 over the three-glyph TrueType program of
_encodingcmap, whose /CIDToGIDMap sends CIDs 1 to 6 to a glyph; P5's also
sends CID 231 to one. Each declares the Adobe-Japan1 supplement its CMap
declares, 4 for the UniJIS CMaps, 2 for 90ms-RKSJ-H and 1 for EUC-V, which
6.2.11.3.1 t1 compares.

| Font | Encoding      | Shows     | CIDs                        | Verdict                       |
| ---- | ------------- | --------- | --------------------------- | ----------------------------- |
| P1   | UniJIS-UCS2-V | 0021 0041 | 2, 34 absent, from the base | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| P2   | UniJIS-UCS2-V | 0021 0022 | 2, 3, from the base         | silent, the control           |
| P3   | 90ms-RKSJ-H   | 01        | 231 absent, a notdef range  | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| P4   | UniJIS-UCS2-H | E000      | 0, mapped nowhere           | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| P5   | 90ms-RKSJ-H   | 01        | 231, a notdef range         | silent, the control           |
| P6   | EUC-V         | 41 A4A2   | 34, 843 absent, from base   | 6.2.11.4.1 t2 and 6.2.11.8 t1 |

P6 is there for text extraction: no font carries a /ToUnicode, so the text
comes from the CMap composed with the Adobe-Japan1 CID-to-Unicode table, as
PDF.js composes it (ISO 32000-1, 9.10.2). EUC-V's own records hold no <41>
and no <A4A2>, so only its base EUC-H turns them into A and U+3042.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

from _encodingcmap import Fixture, system_info
from _common import Name

JAPAN1 = system_info("Japan1", 4)
JAPAN1_2 = system_info("Japan1", 2)

f = Fixture()
f.font("P1", "LithoPredefVerticalAbsent", Name("/UniJIS-UCS2-V"), b"00210041", info=JAPAN1)
f.font("P2", "LithoPredefVerticalPresent", Name("/UniJIS-UCS2-V"), b"00210022", info=JAPAN1)
f.font("P3", "LithoPredefNotdefAbsent", Name("/90ms-RKSJ-H"), b"01", info=JAPAN1_2)
f.font("P4", "LithoPredefUnmapped", Name("/UniJIS-UCS2-H"), b"E000", info=JAPAN1)
f.font("P5", "LithoPredefNotdefPresent", Name("/90ms-RKSJ-H"), b"01", info=JAPAN1_2,
       gid_map=f.gid_map([1, 2, 3, 4, 5, 6, 231]))
f.font("P6", "LithoPredefVerticalText", Name("/EUC-V"), b"41A4A2", info=system_info("Japan1", 1))
f.save("cmappredef_2b.pdf")
