#!/usr/bin/env python3
"""Build cmapnotdef_2b.pdf: encoding CMaps with notdef ranges.

A notdefrange sends every code of its range to one CID (Adobe Technical Note
#5014). veraPDF's CMap.toCID reads it once no cidchar or cidrange maps the
code, the first range covering the code giving its CID. The two corpus files
whose encoding CMaps declare a notdef range show no code it covers, so no count
reached this.

| Font | Encoding CMap                                          | Shows     | CIDs | Verdict                       |
| ---- | ------------------------------------------------------ | --------- | ---- | ----------------------------- |
| N1   | cidchar 0041 to 5, notdefrange 0000 to 00FF to CID 0   | 0041 0043 | 5, 0 | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| N2   | cidrange 0000 to 0006, notdefrange 0100 to 01FF to 3   | 0002 0150 | 2, 3 | silent, the control           |

N1's code 0043 reaches CID 0, which is .notdef, and absent as well: veraPDF's
CIDFontType2Program.containsCID refuses CID 0 and GFGlyph exempts only code 0.

Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
authoritative expected firing set, and _encodingcmap for the font program.
"""

from _encodingcmap import Fixture

f = Fixture()
f.font("N1", "LithoNotdefZero", f.cmap(
    "LithoN1", b"1 begincidchar\n<0041> 5\nendcidchar\n1 beginnotdefrange\n<0000> <00FF> 0\nendnotdefrange\n"),
    b"00410043")
f.font("N2", "LithoNotdefPresent", f.cmap(
    "LithoN2", b"1 begincidrange\n<0000> <0006> 0\nendcidrange\n1 beginnotdefrange\n<0100> <01FF> 3\nendnotdefrange\n"),
    b"00020150")
f.save("cmapnotdef_2b.pdf")
