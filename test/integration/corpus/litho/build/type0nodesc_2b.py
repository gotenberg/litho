#!/usr/bin/env python3
"""Build type0nodesc_2b.pdf: Type 0 fonts with no descendant CIDFont dictionary.

veraPDF builds a Type 0 font over its descendant's dictionary
(PDType0Font.getDescendantCOSDictionary): the first entry of /DescendantFonts
when that entry is a dictionary, and an empty dictionary otherwise. Its /Type
and /BaseFont are read there, so a font with no descendant dictionary fails the
font-dictionary key rules for /Type (ISO 19005-1, 6.3.2 t1, ISO 19005-2 and -3,
6.2.11.2 t1, ISO 19005-4, 6.2.10.2 t1) and for /BaseFont (t3), whatever its own
dictionary declares. Each font below writes /Type /Font and a /BaseFont of its
own, and shows one code:

| Font | /DescendantFonts                    | Verdict                    |
| ---- | ----------------------------------- | -------------------------- |
| C1   | [ a CIDFontType2 dictionary ]       | silent, the control        |
| N1   | [ null ]                            | t1 and t3                  |
| N2   | absent                              | t1 and t3                  |
| N3   | [ ]                                 | t1 and t3                  |
| N4   | a CIDFontType2 dictionary, no array | t1 and t3                  |

No corpus file but pdfjs issue12823.pdf, which no gated corpus holds, writes a
Type 0 font with no descendant dictionary.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

from _common import Array, Dictionary, Name
from _encodingcmap import IDENTITY_SYSTEM_INFO, Fixture

import _truetype

f = Fixture()
f.font("C1", "LithoType0Control", Name("/Identity-H"), b"0001")


def bare(key, name, **descendants):
    """Add the Type 0 font key with the /DescendantFonts entry descendants
    gives, showing code 1."""
    font = f.pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/" + name),
                                          Encoding=Name("/Identity-H"), **descendants))
    f.fonts.append((key, font, b"0001"))


bare("N1", "LithoType0Null", DescendantFonts=Array([None]))
bare("N2", "LithoType0Absent")
bare("N3", "LithoType0Empty", DescendantFonts=Array([]))
bare("N4", "LithoType0NoArray", DescendantFonts=f.pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.CIDFontType2, BaseFont=Name("/LithoType0NoArray"),
    CIDSystemInfo=IDENTITY_SYSTEM_INFO, DW=_truetype.ADVANCE_WIDTH)))
f.save("type0nodesc_2b.pdf")
