#!/usr/bin/env python3
"""Build sharedimage_2b.pdf: images page 1 shares with page 2, at PDF/A-2b.

A redaction blanks the samples a region covers by rewriting the image, which
every other placement of the image shows too. Where the regions do not cover
the same samples at every placement, litho blanks a copy instead: the copy's
covered samples are cleared, the copy joins page 1's resources under a new
name, and only the covered Do paints it. No corpus file in the litho corpus
painted one image in two places, so no cell reached the copy.

Both images are 4 by 4 DeviceGray, which the RGB output intent covers. Each
page names them through the one /Resources dictionary both pages share:

| Image   | Samples | Soft mask        | Page 1                      | Page 2       |
| ------- | ------- | ---------------- | --------------------------- | ------------ |
| `Logo`  | 0x80    | none             | at (50, 250) and (250, 250) | at (50, 250) |
| `Photo` | 0x40    | 4 by 4, all 0xC0 | at (150, 50)                | at (150, 50) |

Every placement is a 100-point square on a 400 by 400 page. A redaction of all
of page 1 covers both of its Logo placements whole and the Photo whole, and
covers neither on page 2, so each of the three placements gets a copy of its
own, the Photo's with a soft mask of its own. The centred quarter of page 1,
[100 100 300 300], covers a different corner of each placement.

See ../README.md for the authoritative expected firing set.
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


def gray(sample, **extra):
    """A 4 by 4 DeviceGray image whose every sample is sample."""
    stream = Stream(pdf, bytes([sample]) * 16)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 4
    stream.Height = 4
    stream.ColorSpace = Name.DeviceGray
    stream.BitsPerComponent = 8
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


logo = gray(0x80)
photo = gray(0x40, SMask=gray(0xC0))
resources = pdf.make_indirect(Dictionary(XObject=Dictionary(Logo=logo, Photo=photo)))


def place(name, x, y):
    return b"q 100 0 0 100 %d %d cm /%s Do Q\n" % (x, y, name)


one = place(b"Logo", 50, 250) + place(b"Logo", 250, 250) + place(b"Photo", 150, 50)
two = place(b"Logo", 50, 250) + place(b"Photo", 150, 50)
for body in (one, two):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 400, 400]),
        Resources=resources,
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "sharedimage_2b.pdf", min_version="1.7")
