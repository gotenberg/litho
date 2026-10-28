#!/usr/bin/env python3
"""Build complex_2u.pdf: PDF/A-2u, the Unicode angle on top of part-2 rules.

Conformance level U adds one demand to level B: every glyph rendered in the
file has to map to Unicode, and the mapping has to be a legal one. ISO
19005-2 6.2.11.7.2 splits that into an absent mapping and a mapping onto a
forbidden code point, and the level-A rule 6.2.11.7.3 adds Private Use Area
text with no /ActualText. All three sit here alongside the part-2 defects.

Three must-not-fire controls guard against over-suppression:
  * a font whose /ToUnicode maps its rendered code to a plain Latin letter,
  * an unrendered code whose glyph name maps to nothing, which the
    usage-scoped glyph rules must skip,
  * a Private Use Area glyph wrapped in a marked-content /ActualText, which
    6.2.11.7.3 exempts.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    lzw_encode,
    pikepdf,
    rgb_image,
    save,
    tounicode_cmap,
    unembedded_truetype,
)

pdf = pikepdf.new()


def simple_font(
    encoding=None,
    tounicode=None,
    first=65,
    last=66,
    widths=(722, 722),
    base="/Helvetica",
):
    """Build a non-embedded Type1 font, optionally with a /ToUnicode CMap."""
    d = Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name(base),
        FirstChar=first,
        LastChar=last,
        Widths=Array(list(widths)),
    )
    d.Encoding = encoding if encoding is not None else Name.WinAnsiEncoding
    if tounicode is not None:
        d.ToUnicode = tounicode
    return pdf.make_indirect(d)


# FNoUni: code 65 is renamed to a glyph name that appears in no standard
# encoding and in no Adobe Glyph List entry, and the font carries no
# /ToUnicode. Its rendered glyph therefore maps to nothing (6.2.11.7.2 t1).
# Code 66 gets the same treatment but is never rendered, so the usage-scoped
# glyph rules must skip it: a firing on it would be a false positive.
# The base font is deliberately not one of the standard 14: a standard-14
# name is taken as proof that every glyph name resolves through the Adobe
# Glyph List, which would make the /Differences array unreachable.
f_no_uni = simple_font(
    base="/LithoCustom",
    encoding=Dictionary(
        Type=Name.Encoding,
        Differences=Array([65, Name("/lithoglyphone"), 66, Name("/lithoglyphtwo")]),
    ),
)

# FZero maps its rendered code onto U+0000, which 6.2.11.7.2 t2 forbids
# along with U+FEFF and U+FFFE.
f_zero = simple_font(tounicode=tounicode_cmap(pdf, {65: 0x0000}), last=65, widths=(722,))

# FPua maps its rendered code onto a Private Use Area value. Rendered bare it
# fails the level-A rule 6.2.11.7.3; rendered inside a marked-content
# /ActualText it is exempt, and the content stream does both.
f_pua = simple_font(tounicode=tounicode_cmap(pdf, {65: 0xE000}), last=65, widths=(722,))

# Control: a well-formed /ToUnicode onto a plain Latin letter. Must stay
# silent while the three above fire.
f_ok = simple_font(tounicode=tounicode_cmap(pdf, {65: 0x0041}), last=65, widths=(722,))

tt = unembedded_truetype()
img = rgb_image(pdf, interpolate=True)

lzw_form = Stream(pdf, b"")
lzw_form.write(lzw_encode(b"0 0 1 rg 5 5 20 20 re f\n"), filter=Name.LZWDecode)
lzw_form.Type = Name.XObject
lzw_form.Subtype = Name.Form
lzw_form.BBox = Array([0, 0, 30, 30])
lzw_form.Resources = Dictionary()

group_form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
group_form.Type = Name.XObject
group_form.Subtype = Name.Form
group_form.BBox = Array([0, 0, 50, 50])
group_form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
group_form.Resources = Dictionary()

psxobj = Stream(pdf, b"%!PS\n")
psxobj.Type = Name.XObject
psxobj.Subtype = Name.PS

content = b"""q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 660 100 30 re f
0.5 g
50 640 100 10 re f
BT /FNoUni 12 Tf 50 610 Td (A) Tj ET
BT /FZero 12 Tf 50 590 Td (A) Tj ET
BT /FPua 12 Tf 50 570 Td (A) Tj ET
/P <</ActualText (guarded)>> BDC
BT /FPua 12 Tf 150 570 Td (A) Tj ET
EMC
BT /FOk 12 Tf 50 550 Td (A) Tj ET
BT /TT1 12 Tf 50 530 Td (AB) Tj ET
q 100 0 0 60 50 440 cm /Im1 Do Q
q 1 0 0 1 50 360 cm /Fm1 Do Q
q 1 0 0 1 150 360 cm /FmLZW Do Q
q 1 0 0 1 250 360 cm /PSX Do Q
Q
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(
            FNoUni=f_no_uni,
            FZero=f_zero,
            FPua=f_pua,
            FOk=f_ok,
            TT1=pdf.make_indirect(tt),
        ),
        XObject=Dictionary(
            Im1=img,
            Fm1=group_form,
            FmLZW=lzw_form,
            PSX=pdf.make_indirect(psxobj),
        ),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

movie = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Movie,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    T=String("clip"),
    Movie=Dictionary(F=String("clip.mov")),
)
hidden = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 600, 500, 650]),
    F=2,
    C=Array([0, 1, 0]),
)
pdf.pages[0].Annots = Array([pdf.make_indirect(movie), pdf.make_indirect(hidden)])

pdf.Root.OpenAction = Dictionary(
    Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('litho');")
)
pdf.Root.NeedsRendering = True
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer")))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg]), D=Dictionary(Order=Array([]), AS=Array([]))
)

# Deliberately no /Metadata on the catalog and no /OutputIntents.
save(pdf, "complex_2u.pdf")
