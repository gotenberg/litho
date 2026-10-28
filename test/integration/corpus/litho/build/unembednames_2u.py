#!/usr/bin/env python3
"""Build unembednames_2u.pdf: the glyph names veraPDF gives the codes of fonts
that embed no program, and the one reading of a Type 3 font's procedures.

A simple font that embeds no program has no names but its encoding's, and
veraPDF takes each shown glyph's Unicode value from the name alone: no name
fails ISO 19005-2, 6.2.11.7.2 t1, the name .notdef has the value U+0000 and
fails t2, and a name the Adobe Glyph List, the Symbol font and ZapfDingbats all
leave out fails t1. Each font rides on its own resource name:

  HV  Helvetica, no /Encoding, shows 01 80 41. StandardEncoding, read from its
      metrics, names 01 and 80 .notdef: t2 and 6.2.11.8 t1 twice each.
  AS  an Arial Type1 under /StandardEncoding, shows 41. veraPDF holds no table
      for that name, so the code has no name: t1 once.
  TS  a symbolic Arial TrueType under /WinAnsiEncoding, shows 00 01 41. Its
      Unicode values read the table, which names 00 and 01 .notdef: t2 twice.
      The .notdef rule ignores a symbolic TrueType's encoding and names code 0
      alone: 6.2.11.8 t1 once.
  ZD  ZapfDingbats, whose /Differences names 41 a71, shows 41: a ZapfDingbats
      name passes both rules here, and fails ISO 19005-1, 6.3.8 t1 at PDF/A-1a.
  T3  a Type 3 font whose glyph a shows code 01 and whose glyph b, never shown,
      code 02, both through the unembedded Helvetica H2. The page shows a in
      rendering mode 3, then in mode 0, and the second page in mode 1. veraPDF
      parses every procedure once, in the mode of the first show: 01 and 02
      fail t2 and 6.2.11.8 t1 once each, and H2, built in mode 3, passes the
      embedding rule.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def descriptor(flags):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name.Arial,
            Flags=flags,
            FontBBox=Array([0, 0, 1000, 1000]),
            ItalicAngle=0,
            Ascent=800,
            Descent=-200,
            CapHeight=700,
            StemV=80,
        )
    )


def arial(subtype, encoding, flags):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name(subtype),
            BaseFont=Name.Arial,
            Encoding=encoding,
            FirstChar=0,
            LastChar=255,
            Widths=Array([500] * 256),
            FontDescriptor=descriptor(flags),
        )
    )


hv = pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.Helvetica))
h2 = pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.Helvetica))
as_ = arial("/Type1", Name.StandardEncoding, 32)
ts = arial("/TrueType", Name.WinAnsiEncoding, 4)
zd = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name.ZapfDingbats,
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.a71])),
    )
)

glyph_a = pdf.make_indirect(Stream(pdf, b"1000 0 d0 BT /H2 10 Tf <01> Tj ET"))
glyph_b = pdf.make_indirect(Stream(pdf, b"1000 0 d0 BT /H2 10 Tf <02> Tj ET"))
t3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=glyph_a, b=glyph_b),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a, Name.b])),
        FirstChar=97,
        LastChar=98,
        Widths=Array([1000, 1000]),
        Resources=Dictionary(Font=Dictionary(H2=h2)),
    )
)

fonts = Dictionary(HV=hv, AS=as_, TS=ts, ZD=zd, T3=t3)
first = (
    b"BT 10 700 Td\n"
    b"/HV 12 Tf <018041> Tj\n"
    b"0 -20 Td /AS 12 Tf <41> Tj\n"
    b"0 -20 Td /TS 12 Tf <000141> Tj\n"
    b"0 -20 Td /ZD 12 Tf <41> Tj\n"
    b"0 -20 Td /T3 12 Tf 3 Tr (a) Tj 0 Tr (a) Tj\n"
    b"ET\n"
)
second = b"BT 10 700 Td /T3 12 Tf 1 Tr (a) Tj ET\n"

for body in (first, second):
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
save(pdf, "unembednames_2u.pdf", min_version="1.7")
