#!/usr/bin/env python3
"""Build fontleft_2b.pdf: glyph widths a font program cannot answer, and the
order in which a validator reads a font's program.

Each construct rides on a font of its own, so a check that abandons a font
after its first bad glyph, or condemns a font wholesale, shows as a count:

  CC  a Type0 font over a CIDFontType0 whose CID-keyed CFF charset holds CIDs
      1 and 2. It shows CID 1, whose /W width matches its advance, and CID 3,
      which the charset lacks and /W declares 500 wide against the /DW of
      1000. veraPDF weighs a CID the charset lacks at the default width, so
      CID 3 fails the width rule beside the presence and .notdef rules.
  CK  the same font showing CID 3 with no /W entry for it: the declared
      width is the default too, so the width rule passes. The control.
  CN  a Type1 over a bare CFF whose /Differences name code 65 .notdef, with
      a /Widths entry of 500 against the 600 the program's .notdef advances.
      The charset holds .notdef, so the glyph is present; its width fails.
  T3  a Type 3 font whose encoding names code 66 /b, for which it holds no
      glyph procedure, 500 wide in /Widths. With no procedure the program's
      width is the missing width, 0, so the width rule fails beside presence.
  TS  a Type 3 font under a /FontMatrix of 0.01, whose procedure declares 50
      against a /Widths entry of 50.5. Both widths are compared as written, in
      glyph space, so half a unit apart passes. The control.
  TF  a composite TrueType font the page draws a form with before its own
      text uses it. At PDF/A veraPDF reads the program for the form, which
      shows CIDs 5, 6 and 8, all past the three-glyph program, and not for the
      page text after it, which shows CID 7: the presence rule fails three
      times, where judging the page text first fails it once. At PDF/UA the
      form claims the font without its glyphs being judged, and neither fails.
  TA  a composite TrueType font the page text shows CID 5 through, and an
      annotation appearance CIDs 6 and 7. At PDF/A-2 veraPDF reads the program
      for the page text, so the presence rule fails once; at PDF/A-1 and
      PDF/UA it parses the annotation first and fails it twice.
  TM  a composite TrueType font shown only in a form drawn while rendering
      mode 3 is in force. The form inherits the mode, so its missing CID 5 is
      exempt from the presence rule and still fails the .notdef rule.
  TN  a Type0 font with no /BaseFont of its own over a descendant that names
      one. veraPDF reads a Type0 font's name from its descendant, so the
      /BaseFont rule passes. The control.

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
CID_CFF_PROGRAM = _cff.build_cid([1, 2])


def program(data, **keys):
    stream = Stream(pdf, data)
    for key, value in keys.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


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


def type0(name, descendant, base_font=True):
    font = Dictionary(
        Type=Name.Font,
        Subtype=Name.Type0,
        Encoding=Name("/Identity-H"),
        DescendantFonts=Array([pdf.make_indirect(descendant)]),
    )
    if base_font:
        font.BaseFont = Name("/" + name)
    return pdf.make_indirect(font)


def cid_info():
    return Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0)


def cff_cid_font(name, widths):
    """A Type0 font over a CIDFontType0 embedding the CID-keyed CFF."""
    descendant = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType0,
        BaseFont=Name("/" + name),
        CIDSystemInfo=cid_info(),
        FontDescriptor=descriptor(name, FontFile3=program(CID_CFF_PROGRAM, Subtype=Name.CIDFontType0C)),
        DW=1000,
        W=widths,
    )
    return type0(name, descendant)


def tt_cid_font(name, base_font=True):
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
    return type0(name, descendant, base_font)


def type3(name, matrix, differences, widths, proc):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            Name=Name("/" + name),
            FontBBox=Array([0, 0, 1000, 1000]),
            FontMatrix=Array(matrix),
            CharProcs=Dictionary(a=program(proc)),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array(differences)),
            FirstChar=65,
            LastChar=64 + len(widths),
            Widths=Array(widths),
            Resources=Dictionary(),
        )
    )


fonts = Dictionary(
    CC=cff_cid_font("LithoCIDCFF", Array([1, Array([600]), 3, Array([500])])),
    CK=cff_cid_font("LithoCIDCFFDefault", Array([1, Array([600])])),
    CN=pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type1,
            BaseFont=Name("/LithoCFFNotdef"),
            FirstChar=65,
            LastChar=66,
            Widths=Array([500, _cff.ADVANCE_WIDTH]),
            Encoding=Dictionary(
                Type=Name.Encoding,
                BaseEncoding=Name.WinAnsiEncoding,
                Differences=Array([65, Name("/.notdef")]),
            ),
            FontDescriptor=descriptor("LithoCFFNotdef", FontFile3=program(CFF_PROGRAM, Subtype=Name.Type1C)),
        )
    ),
    T3=type3("LithoT3", [0.001, 0, 0, 0.001, 0, 0], [65, Name.a, Name.b], [600, 500],
             b"600 0 0 0 500 700 d1 0 0 500 700 re f"),
    TS=type3("LithoT3Scaled", [0.01, 0, 0, 0.01, 0, 0], [65, Name.a], [50.5],
             b"50 0 0 0 50 70 d1 0 0 50 70 re f"),
    TF=tt_cid_font("LithoCIDFormFirst"),
    TA=tt_cid_font("LithoCIDAnnotation"),
    TM=tt_cid_font("LithoCIDInherited"),
    TN=tt_cid_font("LithoCIDUnnamed", base_font=False),
)


def form(body, font_names):
    """A Form XObject naming the listed page fonts in its own resources."""
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 612, 792])
    stream.Resources = Dictionary(Font=Dictionary({"/" + n: fonts["/" + n] for n in font_names}))
    return pdf.make_indirect(stream)


form_first = form(b"BT /TF 12 Tf 50 500 Td <000500060008> Tj ET", ["TF"])
inherited = form(b"BT /TM 12 Tf 50 300 Td <0005> Tj ET", ["TM"])

appearance = Stream(pdf, b"BT /TA 12 Tf 10 10 Td <00060007> Tj ET")
appearance.Type = Name.XObject
appearance.Subtype = Name.Form
appearance.BBox = Array([0, 0, 100, 40])
appearance.Resources = Dictionary(Font=Dictionary(TA=fonts.TA))
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Stamp,
        Rect=Array([400, 700, 500, 740]),
        F=4,
        AP=Dictionary(N=pdf.make_indirect(appearance)),
    )
)

content = (
    b"BT /CC 12 Tf 50 740 Td <00010003> Tj ET\n"
    b"BT /CK 12 Tf 50 710 Td <0003> Tj ET\n"
    b"BT /CN 12 Tf 50 680 Td (AB) Tj ET\n"
    b"BT /T3 12 Tf 50 650 Td (AB) Tj ET\n"
    b"BT /TS 12 Tf 50 620 Td (A) Tj ET\n"
    # The form shows TF first, so its glyphs are the ones judged.
    b"q /FmFirst Do Q\n"
    b"BT /TF 12 Tf 50 590 Td <0007> Tj ET\n"
    # TA is shown on the page here and in the annotation appearance.
    b"BT /TA 12 Tf 50 560 Td <0005> Tj ET\n"
    b"BT /TN 12 Tf 50 530 Td <0001> Tj ET\n"
    # Rendering mode 3 set outside the form, inherited by it, then reset.
    b"BT 3 Tr ET q /FmInherited Do Q BT 0 Tr ET\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=fonts,
            XObject=Dictionary(FmFirst=form_first, FmInherited=inherited),
        ),
        Contents=Stream(pdf, content),
        Annots=Array([annot]),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata, both independent of the font
# clauses above so a font verdict stays visibly separate from them.
save(pdf, "fontleft_2b.pdf", min_version="1.7")
