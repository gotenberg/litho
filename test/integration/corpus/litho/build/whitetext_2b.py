#!/usr/bin/env python3
"""Build whitetext_2b.pdf: white text on a coloured bar, an image and the bare
page, at PDF/A-2b.

Text extraction judged text filled in device white invisible, as white on a
white page, whatever lay under it, so the default reading dropped a white
heading on a coloured bar. A rendering of the page now settles it: the glyphs
that change pixels no other glyph covers show. Rendering mode 3 paints nothing
(ISO 32000-1, 9.3.6, Table 106), so an OCR layer stays invisible without one.
Every glyph is a Type 3 glyph whose description paints a block in the text's
colour, with a /ToUnicode map, so the text extracts and draws the same in every
renderer:

| Run      | Drawn                                         | Default reading |
| -------- | --------------------------------------------- | --------------- |
| `HEAD`   | white, on a navy bar drawn before it          | read            |
| `PHOTO`  | white, on a dark image drawn before it        | read            |
| `BLANK`  | white, on the bare page                       | dropped         |
| `OCR`    | white in mode 3, on the navy bar              | dropped         |
| `BODY`   | black, on the bare page                       | read            |
| `HIDDEN` | white, on the bare page, a navy bar after it  | dropped         |

`HIDDEN` sits under a bar the page paints after it: what the page paints after
a glyph can hide it, never show it, so it stays out with or without a
rendering.

The file declares an RGB output intent, so its DeviceRGB colours are in
order, and it is conforming at PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, tounicode_cmap, type3_font, xmp_packet
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

LETTERS = "ABCDEHIKLNOPRTY"
GLYPHS = [(letter, 700, "50 0 600 700 re f") for letter in LETTERS]
font = type3_font(pdf, GLYPHS, [700] * len(GLYPHS))
font.ToUnicode = tounicode_cmap(pdf, {65 + i: ord(letter) for i, letter in enumerate(LETTERS)})
font = pdf.make_indirect(font)


def shown(word):
    """The codes of word in the font, which numbers LETTERS from 65 on."""
    return bytes(65 + LETTERS.index(c) for c in word)


image = Stream(pdf, bytes([0x20, 0x30, 0x40]) * 4)
image.Type = Name.XObject
image.Subtype = Name.Image
image.Width = 2
image.Height = 2
image.ColorSpace = Name.DeviceRGB
image.BitsPerComponent = 8
image = pdf.make_indirect(image)


def text(word, x, y, colour, mode=0):
    return b"BT %d Tr %s rg /F 20 Tf %d %d Td <%s> Tj ET\n" % (mode, colour, x, y, shown(word).hex().encode())


WHITE = b"1 1 1"
BLACK = b"0 0 0"
content = (
    b"0 0 0.5 rg 40 330 320 40 re f\n"
    + text("HEAD", 50, 340, WHITE)
    + b"q 320 0 0 40 40 270 cm /Im Do Q\n"
    + text("PHOTO", 50, 280, WHITE)
    + text("BLANK", 50, 220, WHITE)
    + b"0 0 0.5 rg 40 150 320 40 re f\n"
    + text("OCR", 50, 160, WHITE, mode=3)
    + text("BODY", 50, 100, BLACK)
    + text("HIDDEN", 50, 40, WHITE)
    + b"0 0 0.5 rg 40 30 320 40 re f\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(Font=Dictionary(F=font), XObject=Dictionary(Im=image)),
    Contents=pdf.make_indirect(Stream(pdf, content)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "whitetext_2b.pdf", min_version="1.7")
