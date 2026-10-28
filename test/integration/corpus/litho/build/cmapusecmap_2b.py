#!/usr/bin/env python3
"""Build cmapusecmap_2b.pdf: an encoding CMap built on a predefined base.

The usecmap operator of an embedded encoding CMap names a predefined CMap whose
mappings the program builds on (ISO 32000-1, 9.7.5.4). veraPDF's CMapParser
loads the base and CMap.toCID reads a code the program leaves unmapped through
it, sending a code no CMap maps to CID 0 (9.7.6.3). No corpus file runs usecmap
in an encoding CMap, so no count reached this.

Two fonts each run `/Identity-H usecmap` and map nothing themselves but a
bfchar, which maps to Unicode, not to CIDs (9.10.3):

| Font | Shows     | CIDs        | Verdict                        |
| ---- | --------- | ----------- | ------------------------------ |
| U1   | 0005 0009 | 5, 9 absent | 6.2.11.4.1 t2 and 6.2.11.8 t1  |
| U2   | 0001 0006 | 1, 6        | silent, the control            |

Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
authoritative expected firing set, and _encodingcmap for the font program.
"""

from _encodingcmap import BFCHAR, Fixture

f = Fixture()
f.font("U1", "LithoUseCMapAbsent", f.cmap("LithoU1", b"/Identity-H usecmap\n" + BFCHAR), b"00050009")
f.font("U2", "LithoUseCMapPresent", f.cmap("LithoU2", b"/Identity-H usecmap\n" + BFCHAR), b"00010006")
f.save("cmapusecmap_2b.pdf")
