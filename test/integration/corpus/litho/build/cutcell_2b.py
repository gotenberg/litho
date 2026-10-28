#!/usr/bin/env python3
"""Build cutcell_2b.pdf: an image whose samples the centred quarter's edges
cut beside their centres, at PDF/A-2b.

A sample paints the whole cell of the unit square it maps onto (ISO 32000-1,
8.9.4), and redaction blanked only the samples whose centres a region holds.
A sample whose centre lies just outside the region kept painting the part of
its cell inside it, up to half a sample deep: the pixel check refused ("pixels
inside the region still paint removed content"), and with the check off the
picture stayed inside the region. Corpus images are fine enough that the part
stays within the check's two-pixel band, so no scenario of the redact-render
cell reached it.

The page is 400 by 400, its centred quarter [100 100 300 300]. One 12 by 10
DeviceGray image, not interpolated, is drawn 360 points square at (19, 2.8),
its samples 30 points wide and 36 high, a checker of white and light grey.
The quarter's left edge cuts the cells of column 2 (x 79 to 109) 6 points past
their centres and its right edge those of column 9 (289 to 319) 4 points short
of theirs; its bottom edge cuts the cells of row 7 from the top (y 74.8 to
110.8) 7.2 points past their centres and its top edge those of row 1 (290.8 to
326.8) 8.8 points short of theirs. Each edge leaves a strip of picture 9 to 11
points deep inside the region where blanking by centres alone keeps it. The
band scenario, [190 210] on a page with no text, lies inside the cells of row
4 and holds their centres.

The RGB output intent covers DeviceGray.

See ../FIXTURES.md for the authoritative expected firing set.
"""

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

WIDTH, HEIGHT = 12, 10
samples = bytes(0xFF if (x + y) % 2 == 0 else 0xC0 for y in range(HEIGHT) for x in range(WIDTH))
image = Stream(pdf, samples)
image.Type = Name.XObject
image.Subtype = Name.Image
image.Width = WIDTH
image.Height = HEIGHT
image.ColorSpace = Name.DeviceGray
image.BitsPerComponent = 8

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(XObject=Dictionary(Im=pdf.make_indirect(image))),
    Contents=pdf.make_indirect(Stream(pdf, b"q 360 0 0 360 19 2.8 cm /Im Do Q\n")),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "cutcell_2b.pdf", min_version="1.7")
