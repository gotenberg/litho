#!/usr/bin/env python3
"""Build cmapusevert_1b.pdf: an encoding CMap whose /UseCMap names V.

ISO 19005-1, 6.3.3.3 requires every CMap but Identity-H and Identity-V
embedded, a base a /UseCMap entry names among them (ISO 32000-1, 9.7.5.3,
Table 120). Remediation embeds a predefined base it ships. V holds only its
vertical forms and takes the rest of its mappings and its code space from H
through usecmap, so a copy of V's own records maps 53 codes and splits none:
the embedded base has to carry the chain flattened, with V's /WMode 1. No
corpus file names a base through /UseCMap, so no count reached this.

The font is a CIDFontType2 over Adobe-Japan1-1 and the three-glyph TrueType
program of _encodingcmap. Its encoding CMap maps 2121 to CID 1 itself and
shows it, so only the base's embedding answers:

| Font | Encoding CMap                                 | Shows | CIDs | Verdict at PDF/A-1b |
| ---- | --------------------------------------------- | ----- | ---- | ------------------- |
| V1   | cidchar 2121 to 1, `/UseCMap /V`, Adobe-Japan1 | 2121  | 1    | 6.3.3.3 t1          |

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

from _encodingcmap import Fixture, system_info
from _common import Name

JAPAN1 = system_info("Japan1", 1)

f = Fixture()
f.font("V1", "LithoUseVertical", f.cmap(
    "LithoV1", b"1 begincidchar\n<2121> 1\nendcidchar\n", use_cmap=Name("/V"), info=JAPAN1),
    b"2121", info=JAPAN1)
f.save("cmapusevert_1b.pdf")
