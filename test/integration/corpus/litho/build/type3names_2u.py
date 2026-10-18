#!/usr/bin/env python3
"""Build type3names_2u.pdf: Type 3 fonts with no /ToUnicode whose glyph names
are the only statement of what their glyphs mean.

ISO 19005-1, 6.3.8 asks a PDF/A-1 level A font for a /ToUnicode CMap, and
veraPDF's PDF/A-1 flavour reads a Type 3 glyph's Unicode value from the CMap
alone. ISO 19005-2, 6.2.11.7.2 and ISO 14289, 7.21.7 and 8.4.5.8 accept a glyph
name veraPDF finds in the Adobe Glyph List, the Symbol font or ZapfDingbats,
and refuse a uniXXXX spelling. Each font rides on its own resource name:

  TN  a Type 3 font whose /Differences names 41 A, 42 alpha, 43 a12 and 44
      uni00E9, all shown. Every name states a value, so remediation writes a
      /ToUnicode from them: 6.3.8 at PDF/A-1a, and 44 at PDF/A-2u and PDF/UA,
      clear.
  TP  a Type 3 font whose /Differences names 41 A and 42 g12, both shown. g12
      is the producer's own name and states nothing, so remediation writes no
      /ToUnicode: a map leaving 42 out would claim a repair veraPDF does not
      see.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def type3(names):
    proc = pdf.make_indirect(Stream(pdf, b"1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f"))
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary({"/" + n: proc for n in names}),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65] + [Name("/" + n) for n in names])),
            FirstChar=65,
            LastChar=64 + len(names),
            Widths=Array([1000] * len(names)),
            Resources=Dictionary(),
        )
    )


fonts = Dictionary(TN=type3(["A", "alpha", "a12", "uni00E9"]), TP=type3(["A", "g12"]))
body = b"BT 10 700 Td\n/TN 12 Tf (ABCD) Tj\n0 -20 Td /TP 12 Tf (AB) Tj\nET\n"
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=fonts),
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "type3names_2u.pdf", min_version="1.4")
