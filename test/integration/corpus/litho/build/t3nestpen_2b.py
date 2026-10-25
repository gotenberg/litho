#!/usr/bin/env python3
"""Build t3nestpen_2b.pdf: nested Type 3 glyphs that stroke with the pen of the page showing them, at PDF/A-2b.

A Type 3 glyph description runs under the graphics state at its show (ISO
32000-1, 9.6.5). A glyph of a second Type 3 font that the description shows
runs under the description's state in turn, which is the page's for every
line parameter the description leaves unset: a stroke of the nested glyph
that sets no width of its own strokes with the page's width (8.4.3.2).
Redaction bounded the nested glyph through the extractor's state, which
starts at the initial pen, so the outer glyph shown under a wide pen was
bounded by the nested line alone. No corpus file shows a nested Type 3 glyph
stroking with an inherited pen.

The page, 400 by 400, shows glyphs of font O at Tf 40, which maps glyph space
by a thousandth to 0.04 point per unit. Each shows glyph `L` of font I at Tf
1000, one unit of I's glyph space to one of O's, and `L` strokes a line at y
500 without setting its pen:

| Glyph | Description                       | Shown at | Under    | Pen reaches        |
| ----- | --------------------------------- | -------- | -------- | ------------------ |
| `A`   | shows `L`                         | 130 60   | `2000 w` | y 40 to 120        |
| `B`   | sets `40 w`, then shows `L`       | 230 60   | `2000 w` | y 79.2 to 80.8     |
| `A`   | the same glyph                    | 130 320  | `1 w`    | y 339.98 to 340.02 |

The centred quarter [100 100 300 300] meets 20 points of the first `A`'s pen,
and neither its line nor the box its line gives it. `B` sets the pen its
nested glyph inherits and lies clear of it, and so does the second `A`, under
the initial pen.

Both fonts state /Resources of their own, and the RGB output intent covers
DeviceGray, so the file is conforming at PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

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

inner = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(L=pdf.make_indirect(Stream(pdf, b"1000 0 d0\n0 500 m 1000 500 l S\n"))),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([76, Name.L])),
        FirstChar=76,
        LastChar=76,
        Widths=Array([1000]),
        Resources=Dictionary(),
        ToUnicode=tounicode_cmap(pdf, {0x4C: 0x4C}),
    )
)
outer = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(
            A=pdf.make_indirect(Stream(pdf, b"1000 0 d0\nBT /I 1000 Tf (L) Tj ET\n")),
            B=pdf.make_indirect(Stream(pdf, b"1000 0 d0\n40 w BT /I 1000 Tf (L) Tj ET\n")),
        ),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65, Name.A, Name.B])),
        FirstChar=65,
        LastChar=66,
        Widths=Array([1000, 1000]),
        Resources=Dictionary(Font=Dictionary(I=inner)),
        ToUnicode=tounicode_cmap(pdf, {0x41: 0x41, 0x42: 0x42}),
    )
)

content = (
    b"0 G 2000 w\n"
    b"BT /O 40 Tf 1 0 0 1 130 60 Tm (A) Tj 1 0 0 1 230 60 Tm (B) Tj ET\n"
    b"1 w\n"
    b"BT /O 40 Tf 1 0 0 1 130 320 Tm (A) Tj ET\n"
)
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(Font=Dictionary(O=outer)),
    Contents=pdf.make_indirect(Stream(pdf, content)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "t3nestpen_2b.pdf", min_version="1.7")
