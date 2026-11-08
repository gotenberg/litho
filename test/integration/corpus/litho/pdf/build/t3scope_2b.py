#!/usr/bin/env python3
"""Build t3scope_2b.pdf: Type 3 glyphs that paint through the resources of the stream showing them, at PDF/A-2b.

A Type 3 font's /Resources is optional (ISO 32000-1, 9.6.5, Table 112): a
glyph description without them takes the resources of the stream that shows
the glyph, a PDF 1.1 practice viewers follow. Redaction bounded such a glyph
once, when its font loaded, without those resources, so a glyph that shows
text or paints a form named only by the page was bounded by the whole page,
and any region on the page removed every glyph of the font. pdfjs pr4922 is
the one corpus file that reaches it.

The page, 400 by 400, shows nine glyphs of font T3, which states no
/Resources, at Tf 40 from 20 200, and again from 20 20 inside form Fm:

| Glyph  | Description              | Through the page                    | Through form Fm                    |
| ------ | ------------------------ | ----------------------------------- | ---------------------------------- |
| T3 `A` | shows `A` in font F1     | an embedded TrueType rectangle      | the same font                      |
| T3 `B` | paints the form `Bar`    | a bar 32 by 28 at its origin        | a bar 32 by 60, Fm's own `Bar`     |

The centred quarter [100 100 300 300] meets the page's glyphs at 100 to 300;
the page's glyphs at 20, 60 and 340 and every glyph Fm shows, its bars
reaching 80, lie clear of it. Bounded by the page, every glyph met it, and
the glyphs Fm draws refused the redaction.

The RGB output intent covers DeviceGray, so the file is conforming at
PDF/A-2b.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, tounicode_cmap, xmp_packet
from _icc import rgb_display_profile

pdf = pikepdf.new()

destination = Stream(pdf, rgb_display_profile())
destination.N = 3
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho RGB display"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)

SYMBOLIC = 4  # ISO 32000-1, Table 123
program = _truetype.build([(3, 0, {0xF041: "A", 0xF042: "B"})])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
truetype = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRect"),
        FirstChar=0x41,
        LastChar=0x42,
        Widths=Array([_truetype.ADVANCE_WIDTH] * 2),
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRect"),
                Flags=SYMBOLIC,
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile2=pdf.make_indirect(font_file),
            )
        ),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([0x41, Name.A, Name.B])),
        ToUnicode=tounicode_cmap(pdf, {0x41: 0x41, 0x42: 0x42}),
    )
)


def bar(height):
    """A form filling a bar 800 wide and height high in glyph space."""
    form = Stream(pdf, b"0 g 100 0 800 %d re f\n" % height)
    form.Type = Name.XObject
    form.Subtype = Name.Form
    form.BBox = Array([100, 0, 900, height])
    form.Resources = Dictionary()
    return pdf.make_indirect(form)


procs = Dictionary(
    A=pdf.make_indirect(Stream(pdf, b"1000 0 d0\nBT /F1 1000 Tf (A) Tj ET\n")),
    B=pdf.make_indirect(Stream(pdf, b"1000 0 d0\n/Bar Do\n")),
)
type3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=procs,
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A, Name.B])),
        FirstChar=65,
        LastChar=66,
        Widths=Array([1000, 1000]),
        ToUnicode=tounicode_cmap(pdf, {0x41: 0x41, 0x42: 0x42}),
    )
)

# The form shows other text: the page text a region removes must not survive in it.
form = Stream(pdf, b"0 g BT /T3 40 Tf 1 0 0 1 20 20 Tm (BBBAAABBB) Tj ET\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 400, 400])
form.Resources = Dictionary(Font=Dictionary(T3=type3, F1=truetype), XObject=Dictionary(Bar=bar(1500)))

content = b"0 g BT /T3 40 Tf 1 0 0 1 20 200 Tm (ABABABABA) Tj ET\n/Fm Do\n"
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(
        Font=Dictionary(T3=type3, F1=truetype),
        XObject=Dictionary(Bar=bar(700), Fm=pdf.make_indirect(form)),
    ),
    Contents=pdf.make_indirect(Stream(pdf, content)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "t3scope_2b.pdf", min_version="1.7")
