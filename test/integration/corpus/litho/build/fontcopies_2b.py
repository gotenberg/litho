#!/usr/bin/env python3
"""Build fontcopies_2b.pdf: fonts written directly, and CIDs past a program.

A font dictionary written directly in a /Font resource dictionary has no
object number, and veraPDF tells its copies apart by the dictionary that
writes each one: every dictionary is a font of its own, judged once however
many pages or forms reach it. Twelve corpus files write a font directly, and
judging each copy rather than the first page that names it moves no count, so
no fixture pinned the reading.

Three fonts are written directly, each a distinct defect:

  U  an unembedded Helvetica, ISO 19005-2, 6.2.11.4.1 t1 once per copy;
  W  a Type 1 subset over a bare CFF whose /Widths say 100 where the program
     says 600, 6.2.11.5 t1 once per glyph shown, two per copy;
  C  a Type0 over a shared CIDFontType2 of three glyphs, showing CID 5, which
     names no glyph: 6.2.11.4.1 t2 and 6.2.11.8 t1 once per copy, and, since
     /DW 1000 disagrees with the 600 glyph 0 advances, 6.2.11.5 t1 too.

They sit in nine layouts, each on pages of its own, 13 copies in all:

| Layout                                             | Copies |
| -------------------------------------------------- | ------ |
| two pages, a copy in each page's resources         | 2      |
| two pages sharing one indirect resource dictionary | 1      |
| two pages sharing one indirect /Font dictionary    | 1      |
| two pages inheriting resources from their node     | 1      |
| an indirect font on one page, a copy of it on the next, one name | 2 |
| two names on one page, a copy each                 | 2      |
| one form two pages paint                           | 1      |
| two forms on one page, a copy in each              | 2      |
| two forms sharing one indirect resource dictionary | 1      |

Every rule above counts 13 at PDF/A-2b, W's width rule 26.

A CID a TrueType CIDFont's program holds no glyph for is weighed at the
advance of glyph 0 (veraPDF-validation 1.30, PDFontFile), so it fails the width
rule wherever the declared width disagrees with that advance, beside the
presence and .notdef rules. fontleft_2b.pdf shows such CIDs with /DW set to the
advance, where the width rule stays silent, and pdfjs Brotli-Prototype-FileA.pdf
shows one veraPDF cannot open. Seven more Type0 fonts, indirect, one CID each,
each over the three-glyph program:

| Font | Descendant                                            | Shows | Answers at PDF/A-2b     |
| ---- | ----------------------------------------------------- | ----- | ----------------------- |
| M1   | `/DW 1000 /W [5 [600]]`, the width of glyph 0         | 5     | presence, .notdef       |
| M2   | `/DW 600 /W [5 [500]]`                                | 5     | presence, .notdef, width |
| M3   | no `/DW`, so 1000                                     | 5     | presence, .notdef, width |
| M4   | no `/CIDToGIDMap`, so Identity                        | 5     | presence, .notdef, width |
| M5   | `/DW 1000`, the first CID past the program            | 3     | presence, .notdef, width |
| M6   | `/CIDToGIDMap` stream sending CID 1 to glyph 100      | 1     | presence, .notdef, width |
| M7   | `/CIDToGIDMap` stream of two entries, ending before it | 5    | presence, .notdef, width |

And the rules veraPDF states on a descendant CIDFont count descendant objects,
while the /CIDSystemInfo rule counts composite fonts. Two unembedded Type0
fonts under UniJIS-UCS2-H whose descendant declares Adobe-Korea1 sit on one
page and share one descendant object (6.2.11.4.1 t1 once, 6.2.11.3.1 t1
twice); two pages write the same Type0 font directly, each with its descendant
written in it (t1 twice, 6.2.11.3.1 t1 twice).

Three rules answer beside these: the Korean descendants carry no /Type, which
6.2.11.2 t1 fails 7 times, once per descendant and once more per composite font
read through it; M4 embeds a program with no /CIDToGIDMap, 6.2.11.3.2 t1; and
the two pages inheriting their resources resolve every font through them,
6.2.2 t2.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

import struct

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


def cid_info(ordering="Identity"):
    return Dictionary(Registry=String("Adobe"), Ordering=String(ordering), Supplement=0)


HELVETICA = descriptor("Helvetica")
SUBSET = descriptor("ABCDEF+LithoCFF", FontFile3=program(CFF_PROGRAM, Subtype=Name.Type1C))
TT_FILE = program(TT_PROGRAM, Length1=len(TT_PROGRAM))
SHARED_CID = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/ABCDEF+LithoCID"),
        CIDSystemInfo=cid_info(),
        FontDescriptor=descriptor("ABCDEF+LithoCID", FontFile2=TT_FILE),
        DW=1000,
        CIDToGIDMap=Name.Identity,
    )
)


def font_u():
    return Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.Helvetica, FirstChar=65, LastChar=66,
                      Widths=Array([600, 600]), Encoding=Name.WinAnsiEncoding, FontDescriptor=HELVETICA)


def font_w():
    return Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name("/ABCDEF+LithoCFF"), FirstChar=65,
                      LastChar=66, Widths=Array([100, 100]), Encoding=Name.WinAnsiEncoding, FontDescriptor=SUBSET)


def font_c():
    return Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/ABCDEF+LithoCID"),
                      Encoding=Name("/Identity-H"), DescendantFonts=Array([SHARED_CID]))


def fonts():
    """A /Font dictionary writing the three fonts directly."""
    return Dictionary(U=font_u(), W=font_w(), C=font_c())


TEXT = b"BT /U 12 Tf 10 50 Td (AB) Tj ET BT /W 12 Tf 10 30 Td (AB) Tj ET BT /C 12 Tf 10 10 Td <0005> Tj ET\n"

root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 200, 100])


def attach(parent, kid, pages=1):
    parent.Kids.append(kid)
    n = parent
    while True:
        n.Count = int(n.Count) + pages
        if "/Parent" not in n:
            break
        n = n.Parent


def page(content, resources=None, parent=root):
    d = Dictionary(Type=Name.Page, Parent=parent, Contents=pdf.make_indirect(Stream(pdf, content)))
    if resources is not None:
        d.Resources = resources
    ref = pdf.make_indirect(d)
    attach(parent, ref)
    return ref


def form(resources, body=TEXT):
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 200, 100])
    stream.Resources = resources
    return pdf.make_indirect(stream)


# Two pages, a copy each.
page(TEXT, Dictionary(Font=fonts()))
page(TEXT, Dictionary(Font=fonts()))
# Two pages sharing one indirect resource dictionary.
shared_res = pdf.make_indirect(Dictionary(Font=fonts()))
page(TEXT, shared_res)
page(TEXT, shared_res)
# Two pages sharing one indirect /Font dictionary.
shared_fonts = pdf.make_indirect(fonts())
page(TEXT, Dictionary(Font=shared_fonts))
page(TEXT, Dictionary(Font=shared_fonts))
# Two pages inheriting their resources from their node.
node = pdf.make_indirect(Dictionary(Type=Name.Pages, Kids=Array([]), Count=0, Parent=root,
                                    Resources=Dictionary(Font=fonts())))
attach(root, node, 0)
page(TEXT, parent=node)
page(TEXT, parent=node)
# An indirect font, then a copy of it under the same name.
indirect = Dictionary(U=pdf.make_indirect(font_u()), W=pdf.make_indirect(font_w()), C=pdf.make_indirect(font_c()))
page(TEXT, Dictionary(Font=indirect))
page(TEXT, Dictionary(Font=fonts()))
# Two names on one page.
page(TEXT + TEXT.replace(b"/U ", b"/U2 ").replace(b"/W ", b"/W2 ").replace(b"/C ", b"/C2 "),
     Dictionary(Font=Dictionary(U=font_u(), W=font_w(), C=font_c(), U2=font_u(), W2=font_w(), C2=font_c())))
# One form two pages paint.
one_form = form(Dictionary(Font=fonts()))
page(b"/X1 Do\n", Dictionary(XObject=Dictionary(X1=one_form)))
page(b"/X1 Do\n", Dictionary(XObject=Dictionary(X1=one_form)))
# Two forms, a copy each.
page(b"/X1 Do /X2 Do\n", Dictionary(XObject=Dictionary(X1=form(Dictionary(Font=fonts())),
                                                       X2=form(Dictionary(Font=fonts())))))
# Two forms sharing one indirect resource dictionary.
forms_res = pdf.make_indirect(Dictionary(Font=fonts()))
page(b"/X1 Do /X2 Do\n", Dictionary(XObject=Dictionary(X1=form(forms_res), X2=form(forms_res))))


def missing(name, show, **cid_keys):
    """A Type0 font over its own CIDFontType2 on the three-glyph program."""
    descendant = Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/" + name),
        CIDSystemInfo=cid_info(),
        FontDescriptor=descriptor(name, FontFile2=TT_FILE),
    )
    for key, value in cid_keys.items():
        descendant[Name("/" + key)] = value
    return pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/" + name),
                                        Encoding=Name("/Identity-H"),
                                        DescendantFonts=Array([pdf.make_indirect(descendant)]))), show


def gid_map(*gids):
    return program(b"".join(struct.pack(">H", g) for g in gids))


missing_fonts = [
    missing("LithoMissingW", 5, DW=1000, W=Array([5, Array([_truetype.ADVANCE_WIDTH])]), CIDToGIDMap=Name.Identity),
    missing("LithoMissingWOff", 5, DW=_truetype.ADVANCE_WIDTH, W=Array([5, Array([500])]), CIDToGIDMap=Name.Identity),
    missing("LithoMissingNoDW", 5, CIDToGIDMap=Name.Identity),
    missing("LithoMissingNoMap", 5, DW=1000),
    missing("LithoMissingFirst", 3, DW=1000, CIDToGIDMap=Name.Identity),
    missing("LithoMissingMapped", 1, DW=1000, CIDToGIDMap=gid_map(0, 100)),
    missing("LithoMissingShortMap", 5, DW=1000, CIDToGIDMap=gid_map(0, 1)),
]
missing_res = Dictionary()
missing_text = b""
for index, (font, cid) in enumerate(missing_fonts):
    missing_res[Name("/M%d" % (index + 1))] = font
    missing_text += b"BT /M%d 12 Tf 10 %d Td <%04X> Tj ET\n" % (index + 1, 90 - 10 * index, cid)
page(missing_text, Dictionary(Font=missing_res))

# Descendant rules: two Type0 fonts sharing one unembedded descendant object,
# then one Type0 font written directly on two pages with its descendant in it.
KOREA_DESCRIPTOR = descriptor("LithoKorea")


def korea_descendant():
    return Dictionary(Subtype=Name.CIDFontType2, BaseFont=Name("/LithoKorea"), DW=1000,
                      FontDescriptor=KOREA_DESCRIPTOR, CIDSystemInfo=cid_info("Korea1"))


def korea_type0(descendant):
    return Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/LithoKorea"),
                      Encoding=Name("/UniJIS-UCS2-H"), DescendantFonts=Array([descendant]))


KOREA_TEXT = b"BT /K1 12 Tf 10 50 Td <00410042> Tj ET BT /K2 12 Tf 10 30 Td <00410042> Tj ET\n"
shared_desc = pdf.make_indirect(korea_descendant())
page(KOREA_TEXT, Dictionary(Font=Dictionary(K1=pdf.make_indirect(korea_type0(shared_desc)),
                                            K2=pdf.make_indirect(korea_type0(shared_desc)))))
page(b"BT /K1 12 Tf 10 50 Td <00410042> Tj ET\n", Dictionary(Font=Dictionary(K1=korea_type0(korea_descendant()))))
page(b"BT /K1 12 Tf 10 50 Td <00410042> Tj ET\n", Dictionary(Font=Dictionary(K1=korea_type0(korea_descendant()))))

# Deliberately no /Metadata and no output intent.
save(pdf, "fontcopies_2b.pdf")
