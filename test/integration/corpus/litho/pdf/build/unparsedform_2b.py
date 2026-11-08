#!/usr/bin/env python3
"""Build unparsedform_2b.pdf: an image a form litho cannot parse also draws,
at PDF/A-2b.

A redaction blanks an image in place only when every placement of it had the
same samples covered. It measures the placements by running the page through
the forms it draws, and a form whose content does not parse used to read as
painting nothing: an image such a form also names was blanked in place, and a
viewer that reads the form shows the blank where no region reached. No corpus
file drew a form litho cannot parse, so no cell reached the measurement.

The image is 5 by 5 DeviceGray, which the RGB output intent covers:

| Form     | Draws                                        | Placement                 |
| -------- | -------------------------------------------- | ------------------------- |
| `Card`   | `Logo` (0x80) over its 100-point square      | at (150, 150), 100 points |
| `Broken` | `Logo` over its square, then a 10 MiB string | at (340, 340), 10 points  |

`Broken`'s content holds a string literal past litho's 10 MiB lexer bound,
after the image's Do, so litho parses none of it while poppler draws the
image. Its placement is small enough that the render cell's
page and ink bars hold without it. The centred quarter of the 400 by 400
page, [100 100 300 300], covers `Card` whole and misses `Broken`; the band
through the centre cuts `Card` and misses `Broken`; the whole page covers
both.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import zlib

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
from _icc import rgb_display_profile

pdf = pikepdf.new()
# A document language: litho's PDF/UA-1 language check reads a stream it
# cannot parse as one that may show text with no language (ISO 14289-1, 7.2),
# where veraPDF reads Broken through its string and finds none.
pdf.Root.Lang = String("en")

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

logo = Stream(pdf, bytes([0x80]) * 25)
logo.Type = Name.XObject
logo.Subtype = Name.Image
logo.Width = 5
logo.Height = 5
logo.ColorSpace = Name.DeviceGray
logo.BitsPerComponent = 8
logo = pdf.make_indirect(logo)

DRAW = b"q 100 0 0 100 0 0 cm /Logo Do Q\n"
# One byte past the lexer's maxStringAccum, 10 << 20.
OVERSIZED = b"(" + b"A" * ((10 << 20) + 1) + b")\n"


def form(body, compressed=False):
    """A form XObject over the 100-point square drawing body."""
    stream = Stream(pdf, zlib.compress(body, 9) if compressed else body)
    if compressed:
        stream.Filter = Name.FlateDecode
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 100, 100])
    stream.Resources = Dictionary(XObject=Dictionary(Logo=logo))
    return pdf.make_indirect(stream)


card = form(DRAW)
broken = form(DRAW + OVERSIZED, compressed=True)

body = b"q 1 0 0 1 150 150 cm /Card Do Q\nq 0.1 0 0 0.1 340 340 cm /Broken Do Q\n"
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(XObject=Dictionary(Card=card, Broken=broken)),
    Contents=pdf.make_indirect(Stream(pdf, body)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "unparsedform_2b.pdf", min_version="1.7")
