#!/usr/bin/env python3
"""Build cmapcollect_1b.pdf: a Type 0 font whose /Encoding names Adobe-Korea1-2.

Adobe-Korea1-2 is the CMap of the Adobe-Korea1 character collection at
supplement 2: an identity over the collection's CIDs on the two-byte codes
<0000> to <47FF>, declaring CIDSystemInfo Adobe Korea1 2. It lies outside ISO 32000-1,
9.7.5.2, Table 118, but veraPDF 1.30.2 bundles it, and so do PDF.js's bcmaps,
so both decode the font's codes through it. ISO 19005-1, 6.3.3.3 requires
every CMap but Identity-H and Identity-V embedded, and veraPDF takes a named
CMap's CIDSystemInfo from its CharacterCollections table, which lists only the
Table 118 names, so 6.3.3.1 t1 also fails on the name. The verapdf corpus
holds this shape in its PDF/A-2b, PDF/A-4 and PDF/UA files only, which the
gate never remediates at PDF/A-1.

The font is a CIDFontType2 over Adobe-Korea1-2 and the three-glyph TrueType
program of _encodingcmap, whose /CIDToGIDMap sends CIDs 1 to 6 to a glyph:

| Font | Encoding          | Shows     | CIDs | Verdict at PDF/A-1b       |
| ---- | ----------------- | --------- | ---- | ------------------------- |
| C1   | `/Adobe-Korea1-2` | 0001 0002 | 1, 2 | 6.3.3.1 t1 and 6.3.3.3 t1 |

Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
authoritative expected firing set.
"""

from _encodingcmap import Fixture, system_info
from _common import Name

f = Fixture()
f.font("C1", "LithoCollectionCMap", Name("/Adobe-Korea1-2"), b"00010002", info=system_info("Korea1", 2))
f.save("cmapcollect_1b.pdf")
