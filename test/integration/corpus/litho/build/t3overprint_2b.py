#!/usr/bin/env python3
"""Build t3overprint_2b.pdf: Type 3 glyphs shown under overprint, at PDF/A-2b.

A Type 3 glyph description runs under the graphics state of the text that
shows it (ISO 32000-1, 9.6.5), overprint parameters included (8.6.7).
Converting colour judges what overprinting a painting prints: a colour
converted into DeviceCMYK under overprint mode 1 stops painting the inks of
its zero components, which an /OPM 0 restores, and a DeviceCMYK colour
converted out of it paints them. litho once judged the colour the text is
shown in, never the glyph's own. No corpus file shows a Type 3 glyph under
overprint, so no cell reached the judgement.

The page shows three glyphs of the font T3 under GS0, /OP true and /OPM 1:

| Code | Opens with | Paints                                       |
| ---- | ---------- | -------------------------------------------- |
| `A`  | d0         | 0.5 g: grey, black alone once in DeviceCMYK  |
| `B`  | d0         | 0 1 1 0 k: leaves cyan and black under /OPM 1 |
| `C`  | d1         | the text's colour, 0 0 0 1 k                 |

Converted to RGB, B and C paint every ink where they left two or three, and
both are reported; converted to litho's CMYK printer profile, both move their
zero components, and are reported too. Before, litho judged the show once,
in the text's black, and reported one painting.

The CMYK output intent covers DeviceGray and DeviceCMYK, and overprint mode 1
over DeviceCMYK is allowed at PDF/A-2b (ISO 19005-2, 6.2.4.2 restricts it
over ICCBased CMYK alone).

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, type3_font, xmp_packet
from _icc import cmyk_printer_profile

pdf = pikepdf.new()

destination = Stream(pdf, cmyk_printer_profile())
destination.N = 4
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho CMYK printer"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)

font = type3_font(
    pdf,
    [
        ("A", 1000, "0.5 g 0 0 1000 1000 re f"),
        ("B", 1000, "0 1 1 0 k 0 0 1000 1000 re f"),
    ],
    [1000, 1000, 1000],
)
font.CharProcs.C = Stream(pdf, b"1000 0 0 0 1000 1000 d1\n0 0 1000 1000 re f\n")
font.Encoding.Differences.append(Name.C)
font.LastChar = 67

resources = Dictionary(
    ExtGState=Dictionary(GS0=pdf.make_indirect(Dictionary(Type=Name.ExtGState, OP=True, op=True, OPM=1))),
    Font=Dictionary(T3=pdf.make_indirect(font)),
)
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=resources,
    Contents=pdf.make_indirect(Stream(pdf, b"/GS0 gs 0 0 0 1 k BT /T3 48 Tf 50 300 Td (ABC) Tj ET\n")),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "t3overprint_2b.pdf", min_version="1.7")
