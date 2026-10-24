#!/usr/bin/env python3
"""Build sharedformimg_2b.pdf: an image forms draw on both pages, at PDF/A-2b.

A form's content is in its own stream, drawn by every Do naming the form, so
an image a form paints cannot be blanked for one placement by repointing a Do
on the page. Where the regions do not cover the same samples at every
placement, litho copies the form, and each form between the page and the
image, for the covered placement: the copy names a blanked copy of the image,
and the page draws the copy. No file in the litho corpus drew one image
through a form in two places, so no cell reached the form copy.

The image is 4 by 4 DeviceGray, which the RGB output intent covers. Both pages
name the forms through the one /Resources dictionary they share:

| Form    | Draws                         | Page 1                     | Page 2        |
| ------- | ----------------------------- | -------------------------- | ------------- |
| `Card`  | `Logo` (0x80) over its square | at (50, 250) and (150, 50) | at (50, 250)  |
| `Frame` | `Card` at its origin          | at (250, 250)              | at (250, 250) |

Every placement paints a 100-point square on a 400 by 400 page. A redaction of
all of page 1 covers its three placements whole and none on page 2, so each of
the three gets a copy of the forms above it: one Card copy for each direct
placement, and a Frame copy drawing a Card copy for the nested one. The
centred quarter of page 1, [100 100 300 300], covers a different corner of
each placement.

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

logo = Stream(pdf, bytes([0x80]) * 16)
logo.Type = Name.XObject
logo.Subtype = Name.Image
logo.Width = 4
logo.Height = 4
logo.ColorSpace = Name.DeviceGray
logo.BitsPerComponent = 8
logo = pdf.make_indirect(logo)


def form(body, xobjects):
    """A form XObject over the 100-point square drawing body."""
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 100, 100])
    stream.Resources = Dictionary(XObject=Dictionary(**xobjects))
    return pdf.make_indirect(stream)


card = form(b"q 100 0 0 100 0 0 cm /Logo Do Q\n", {"Logo": logo})
frame = form(b"/Card Do\n", {"Card": card})
resources = pdf.make_indirect(Dictionary(XObject=Dictionary(Card=card, Frame=frame)))


def place(name, x, y):
    return b"q 1 0 0 1 %d %d cm /%s Do Q\n" % (x, y, name)


one = place(b"Card", 50, 250) + place(b"Frame", 250, 250) + place(b"Card", 150, 50)
two = place(b"Card", 50, 250) + place(b"Frame", 250, 250)
for body in (one, two):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 400, 400]),
        Resources=resources,
        Contents=pdf.make_indirect(Stream(pdf, body)),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "sharedformimg_2b.pdf", min_version="1.7")
