#!/usr/bin/env python3
"""Build strokedtext_2b.pdf: text whose pen paints past its outline, at PDF/A-2b.

A stroking text rendering mode (ISO 32000-1, 9.3.6, Table 106: modes 1, 2,
5 and 6) strokes each glyph's outline with the graphics state's line width,
join and miter limit (8.4.3.2 to 8.4.3.5), so a wide pen paints past the box
the glyph's outline is bounded by. Redaction once bounded a glyph by that
box alone: a region over the stroke and clear of the outline selected
nothing, and the stroke stayed under it. No corpus file the redact-render
cell samples strokes text wide enough to reach a region the outline does
not.

The page, 400 by 400, shows one embedded CFF font (`_cff.py`: A and B are
rectangles 400 units wide, 400 and 500 high, advancing 600):

| Run    | At       | Tf | Mode | Pen                        | Reaches past its outline |
| ------ | -------- | -- | ---- | -------------------------- | ------------------------ |
| `A`    | 55 170   | 50 | 1    | 60 wide, round joins       | 30 points                |
| `B`    | 318 170  | 50 | 2    | 4 wide, miter joins, M 10  | 2.8 points at its corners|
| `ABAB` | 130 195  | 20 | 2    | 0.5 wide, the fake bold    | 0.35 at its corners      |

The centred quarter [100 100 300 300] meets the stroke of `A`, 7.5 points
of it, and neither its outline nor its box without the stroke. A glyph's box
grows by half its pen, so `B`'s grows by 2 points, to 16 points clear of
the quarter, which keeps it: its square corners reach 2.8 points past its
outline, inside the 2.5 points of side bearing its box holds. `ABAB` lies
inside the quarter and under the band.

The RGB output intent covers DeviceGray, and the font is embedded, so the
file is conforming at PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

import _cff
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
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

program = Stream(pdf, _cff.build())
program.Subtype = Name("/Type1C")
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/LithoCFF"),
        FirstChar=65,
        LastChar=66,
        Widths=Array([600, 600]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoCFF"),
                Flags=32,
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile3=pdf.make_indirect(program),
            )
        ),
    )
)

content = (
    b"0 g 0 G\n"
    b"q 60 w 1 j BT /F1 50 Tf 1 Tr 1 0 0 1 55 170 Tm (A) Tj ET Q\n"
    b"q 4 w 0 j 10 M BT /F1 50 Tf 2 Tr 1 0 0 1 318 170 Tm (B) Tj ET Q\n"
    b"q 0.5 w BT /F1 20 Tf 2 Tr 1 0 0 1 130 195 Tm (ABAB) Tj ET Q\n"
)
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=pdf.make_indirect(Stream(pdf, content)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "strokedtext_2b.pdf", min_version="1.7")
