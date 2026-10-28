#!/usr/bin/env python3
"""Build fontclaims_2b.pdf: glyphs a name-keyed CFF program lacks, where a
painted pattern and a Type 3 glyph read a font's program, and a control code an
unembedded standard font names .notdef.

Each construct rides on a font of its own, so each count has one source:

  NK  a Type0 font over a CIDFontType0 whose CFF program is name-keyed: it has
      no ROS operator, so a CID is the glyph index itself (ISO 32000-1,
      9.7.4.2), and the three-glyph program lacks CID 3. The font is a subset,
      so veraPDF weighs the missing glyph at the /DW of 1000, and CID 3 fails
      the presence, .notdef and width rules against its /W of 500. CID 1 is
      present and 600 wide, as /W declares.
  NF  the same program under a name with no subset tag, CID 3 declared 600
      wide. veraPDF weighs a missing glyph of such a font at the program's
      defaultWidthX, 600, so the width rule passes where the presence and
      .notdef rules fail.
  PT  a composite TrueType font a tiling pattern's cell shows CID 5 through,
      then the page text CID 6, both past the three-glyph program. veraPDF
      parses the cell where the page fills with the pattern, before the text,
      and judges neither glyph by the presence rule; both fail .notdef.
  TC  a composite TrueType font a Type 3 glyph procedure shows CID 5 through,
      then the page text CIDs 6 and 7. veraPDF parses the procedure where the
      page shows the Type 3 glyph, so the procedure's glyph is the one the
      presence rule judges; all three fail .notdef.
  TF  a composite TrueType font the procedure of a Type 3 glyph a form shows
      draws CID 5 through: reached through the form, it fails the presence and
      .notdef rules.
  HV  an unembedded Helvetica stating no /Encoding and no /FontDescriptor,
      showing code 1. Its built-in encoding, StandardEncoding, names nothing
      there, so the code references .notdef. A /ToUnicode maps the code, so
      the Unicode rules have a value to judge rather than the .notdef name.

Every count was confirmed with live veraPDF 1.30. See ../FIXTURES.md for the
authoritative expected firing set.
"""

import _cff
import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

NONSYMBOLIC = 32
TT_PROGRAM = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])
CFF_PROGRAM = _cff.build()


def program(data, **keys):
    stream = Stream(pdf, data)
    for key, value in keys.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


CFF_STREAM = program(CFF_PROGRAM, Subtype=Name.CIDFontType0C)


def descriptor(name, **font_file):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name("/" + name),
            Flags=NONSYMBOLIC,
            ItalicAngle=0,
            Ascent=800,
            Descent=-200,
            CapHeight=700,
            StemV=80,
            FontBBox=Array([0, -200, 550, 700]),
            **font_file,
        )
    )


def type0(name, descendant):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type0,
            BaseFont=Name("/" + name),
            Encoding=Name("/Identity-H"),
            DescendantFonts=Array([pdf.make_indirect(descendant)]),
        )
    )


def cid_info():
    return Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0)


def name_keyed_font(name, widths):
    """A Type0 font over a CIDFontType0 embedding the name-keyed CFF."""
    descendant = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType0,
        BaseFont=Name("/" + name),
        CIDSystemInfo=cid_info(),
        FontDescriptor=descriptor(name, FontFile3=CFF_STREAM),
        DW=1000,
        W=widths,
    )
    return type0(name, descendant)


def tt_cid_font(name):
    """A Type0 font over a CIDFontType2 embedding the three-glyph TrueType."""
    descendant = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/" + name),
        CIDSystemInfo=cid_info(),
        FontDescriptor=descriptor(name, FontFile2=program(TT_PROGRAM, Length1=len(TT_PROGRAM))),
        DW=_truetype.ADVANCE_WIDTH,
        CIDToGIDMap=Name.Identity,
    )
    return type0(name, descendant)


def type3(name, font_name, font, shows):
    """A Type 3 font whose glyph a shows shows through font, named font_name."""
    proc = b"500 0 d0 BT /" + font_name.encode() + b" 12 Tf 0 0 Td " + shows + b" Tj ET"
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            Name=Name("/" + name),
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(a=program(proc)),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a])),
            FirstChar=97,
            LastChar=97,
            Widths=Array([500]),
            Resources=Dictionary(Font=Dictionary({"/" + font_name: font})),
        )
    )


# Maps HV's code 1 to U+0041, so the Unicode rules have a value for it and the
# .notdef rule alone judges its name.
TO_UNICODE = b"""/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<00> <FF>
endcodespacerange
1 beginbfchar
<01> <0041>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end
"""

PT = tt_cid_font("LithoCIDPattern")
TC = tt_cid_font("LithoCIDType3")
TF = tt_cid_font("LithoCIDType3Form")

fonts = Dictionary(
    NK=name_keyed_font("ABCDEF+LithoNameKeyed", Array([1, Array([600]), 3, Array([500])])),
    NF=name_keyed_font("LithoNameKeyedFull", Array([1, Array([600]), 3, Array([600])])),
    PT=PT,
    TC=TC,
    T3C=type3("LithoT3Claim", "TC", TC, b"<0005>"),
    HV=pdf.make_indirect(
        Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.Helvetica, ToUnicode=program(TO_UNICODE))
    ),
)

cell = Stream(pdf, b"BT /PT 12 Tf 2 2 Td <0005> Tj ET")
cell.Type = Name.Pattern
cell.PatternType = 1
cell.PaintType = 1
cell.TilingType = 1
cell.BBox = Array([0, 0, 20, 20])
cell.XStep = 20
cell.YStep = 20
cell.Resources = Dictionary(Font=Dictionary(PT=PT))

t3_form = Stream(pdf, b"BT /T3F 12 Tf 0 0 Td (a) Tj ET")
t3_form.Type = Name.XObject
t3_form.Subtype = Name.Form
t3_form.BBox = Array([0, 0, 612, 792])
t3_form.Resources = Dictionary(Font=Dictionary(T3F=type3("LithoT3Form", "TF", TF, b"<0005>")))

content = (
    b"BT /NK 12 Tf 50 740 Td <00010003> Tj ET\n"
    b"BT /NF 12 Tf 50 710 Td <0003> Tj ET\n"
    # The fill parses the cell before the text shows PT.
    b"q /Pattern cs /P1 scn 50 600 60 60 re f Q\n"
    b"BT /PT 12 Tf 50 560 Td <0006> Tj ET\n"
    # The Type 3 glyph parses its procedure before the text shows TC.
    b"BT /T3C 12 Tf 50 530 Td (a) Tj ET\n"
    b"BT /TC 12 Tf 50 500 Td <00060007> Tj ET\n"
    b"q /FmT3 Do Q\n"
    b"BT /HV 12 Tf 50 440 Td (\\001) Tj ET\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=fonts,
            Pattern=Dictionary(P1=pdf.make_indirect(cell)),
            XObject=Dictionary(FmT3=pdf.make_indirect(t3_form)),
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata, both independent of the font
# clauses above so a font verdict stays visibly separate from them.
save(pdf, "fontclaims_2b.pdf", min_version="1.7")
