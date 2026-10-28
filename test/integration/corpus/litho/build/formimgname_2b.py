#!/usr/bin/env python3
"""Build formimgname_2b.pdf: a page image and a form XObject's image under one
resource name, at PDF/A-2b.

A form XObject resolves the names its content uses in its own /Resources (ISO
32000-1, 8.10.1, 7.8.3), so the form's Im0 is another image than the page's
Im0. Redaction looked every image name up in the page's resources: a
redaction covering the form's picture blanked the page's Im0 instead, left
the form's picture whole, and reported success. govdocs thread_000_000582 and
thread_400_400358 have this shape, outside the render sample the redact-render
cell draws from.

| Name             | Image                     | Drawn at (page space)           |
| ---------------- | ------------------------- | ------------------------------- |
| page `/Im0`      | 8 x 8 grey ramp           | `[50 600 250 750]`, the page    |
| form `/Fm0 /Im0` | 8 x 8 yellow-cyan checker | `[206 296 406 496]`, by `/Fm0`  |

The form's picture sits at the centre of the page, inside the centred quarter
and the centre band the redact-render cell redacts, and its light colours
show above the cell's dark level once drawn over black.

The file declares no OutputIntent, so DeviceRGB and DeviceGray fail.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def image(samples, colour_space):
    stream = Stream(pdf, samples)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 8
    stream.Height = 8
    stream.ColorSpace = colour_space
    stream.BitsPerComponent = 8
    return pdf.make_indirect(stream)


ramp = bytes(64 + 16 * x for _ in range(8) for x in range(8))
yellow, cyan = b"\xff\xff\x00", b"\x00\xff\xff"
checker = b"".join(yellow if (x + y) % 2 == 0 else cyan for y in range(8) for x in range(8))

page_image = image(ramp, Name.DeviceGray)
form_image = image(checker, Name.DeviceRGB)

form = Stream(pdf, b"q 200 0 0 200 206 296 cm /Im0 Do Q\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 612, 792])
form.Resources = Dictionary(XObject=Dictionary(Im0=form_image))
form = pdf.make_indirect(form)

content = pdf.make_indirect(Stream(pdf, b"q 200 0 0 150 50 600 cm /Im0 Do Q\nq /Fm0 Do Q\n"))
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(XObject=Dictionary(Im0=page_image, Fm0=form)),
    Contents=content,
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "formimgname_2b.pdf", min_version="1.7")
