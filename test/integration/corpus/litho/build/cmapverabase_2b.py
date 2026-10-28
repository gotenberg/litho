#!/usr/bin/env python3
"""Build cmapverabase_2b.pdf: encoding CMaps only veraPDF's bundle resolves.

veraPDF's PDCMap loads the CMap file it bundles under a name, whether a font's
/Encoding, a usecmap operand or a /UseCMap entry names it, and decodes codes to
CIDs through it (ISO 32000-1, 9.7.5.2 and 9.7.5.4). Its bundle holds CMaps
PDF.js ships none of, the character-collection CMaps of the newest supplements
(Adobe-KR-9, Adobe-Japan1-7, Adobe-GB1-6) and UniAKR-UTF16-H, and copies of
Unicode CMaps that map other codes than PDF.js's: UniGB-UTF16-H maps U+2002 to
CID 7716, which PDF.js's leaves unmapped, and UniKS-UTF16-H leaves U+FA2E
unmapped, which PDF.js's maps to CID 4419. A code nothing maps selects CID 0
(9.7.6.3). No corpus file names a base only veraPDF bundles or shows a code
where the two copies differ, so no count reached this.

Every font is a CIDFontType2 over the three-glyph TrueType program of
_encodingcmap, whose /CIDToGIDMap sends CIDs 1 to 6 to a glyph; K6's also
sends CID 7716 to one and K7's CID 4419.

| Font | Encoding                           | Shows     | CIDs              | Verdict                       |
| ---- | ---------------------------------- | --------- | ----------------- | ----------------------------- |
| K1   | `/Adobe-KR-9 usecmap`, bfchar      | 0005 0009 | 5, 9 absent       | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| K2   | `/Adobe-KR-9 usecmap`, bfchar      | 0001 0006 | 1, 6              | silent, the control           |
| K3   | `/Adobe-Japan1-7 usecmap`, bfchar  | 5A20      | 0, past CIDCount  | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| K4   | `/UseCMap /Adobe-GB1-6`, bfchar    | 0007      | 7 absent          | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| K5   | `/UniAKR-UTF16-H`                  | 0041      | 34 absent         | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
| K6   | `/UniGB-UTF16-H`                   | 2002      | 7716              | silent, the control           |
| K7   | `/UniKS-UTF16-H`                   | FA2E      | 0, mapped nowhere | 6.2.11.4.1 t2 and 6.2.11.8 t1 |

K4's /UseCMap and K5's /Encoding name no CMap of Table 118, which 6.2.11.3.3
t3 and t1 answer, and veraPDF knows no CIDSystemInfo for UniAKR-UTF16-H, which
6.2.11.3.1 t1 answers.

Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
authoritative expected firing set, and _encodingcmap for the font program.
"""

from _common import Name
from _encodingcmap import BFCHAR, Fixture, system_info

KR9 = system_info("KR", 9)
JAPAN1_7 = system_info("Japan1", 7)
GB1_6 = system_info("GB1", 6)

f = Fixture()
f.font("K1", "LithoVeraBaseAbsent", f.cmap("LithoK1", b"/Adobe-KR-9 usecmap\n" + BFCHAR, info=KR9), b"00050009",
       info=KR9)
f.font("K2", "LithoVeraBasePresent", f.cmap("LithoK2", b"/Adobe-KR-9 usecmap\n" + BFCHAR, info=KR9), b"00010006",
       info=KR9)
f.font("K3", "LithoVeraBaseUnmapped", f.cmap("LithoK3", b"/Adobe-Japan1-7 usecmap\n" + BFCHAR, info=JAPAN1_7),
       b"5A20", info=JAPAN1_7)
f.font("K4", "LithoVeraUseCMapAbsent", f.cmap("LithoK4", BFCHAR, use_cmap=Name("/Adobe-GB1-6"), info=GB1_6), b"0007",
       info=GB1_6)
f.font("K5", "LithoVeraNamedAbsent", Name("/UniAKR-UTF16-H"), b"0041", info=KR9)
f.font("K6", "LithoVeraRevisedPresent", Name("/UniGB-UTF16-H"), b"2002", info=system_info("GB1", 4),
       gid_map=f.gid_map([1, 2, 3, 4, 5, 6, 7716]))
f.font("K7", "LithoVeraRevisedUnmapped", Name("/UniKS-UTF16-H"), b"FA2E", info=system_info("Korea1", 2),
       gid_map=f.gid_map([1, 2, 3, 4, 5, 6, 4419]))
f.save("cmapverabase_2b.pdf")
