#!/usr/bin/env python3
"""Build imagereach_2b.pdf: images the XObject rules reach only through a Do.

veraPDF judges an image where a Do operator draws it, once for the whole
document, and a page content stream another page shares once, through the
resources of the first page that draws it. Every image in this file breaks two
image rules, an /Intent outside the four ISO 32000-1 names (ISO 19005-2, 6.2.6)
and an /Interpolate of true (6.2.8 t3):

  I1  listed by the resources of the form FmList the first page draws, whose
      content draws nothing. A form's resource entry is not a draw.
  I2  listed by the resources of the form FmNest that FmList's sibling FmOuter
      draws, and drawn by neither.
  I3  drawn by the form FmShared that the second page names /FmList, through
      the content stream it shares with the first page, which draws /FmList
      as the first page names it.

Neither rule fails for I1 or I2, where judging the resource entries fails
both. I3 is drawn, since the second page's /FmList resolves in the second
page's resources (ISO 32000-1, 7.8.3): litho fails both rules once, and
veraPDF, which reads the shared stream through the first page alone, fails
neither.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def image(sample):
    stream = Stream(pdf, sample)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 1
    stream.Height = 1
    stream.ColorSpace = Name.DeviceGray
    stream.BitsPerComponent = 8
    stream.Intent = Name("/Bogus")
    stream.Interpolate = True
    return pdf.make_indirect(stream)


def form(body, xobjects):
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 100, 100])
    stream.Resources = Dictionary(XObject=Dictionary(xobjects))
    return pdf.make_indirect(stream)


i1, i2, i3 = image(b"\x80"), image(b"\x40"), image(b"\xc0")
fm_list = form(b"0 0 10 10 re f", {"/I1": i1})
fm_nest = form(b"0 0 10 10 re f", {"/I2": i2})
fm_outer = form(b"/FmNest Do", {"/FmNest": fm_nest})
fm_shared = form(b"q 10 0 0 10 0 0 cm /I3 Do Q", {"/I3": i3})

content = pdf.make_indirect(Stream(pdf, b"q /FmList Do Q q /FmOuter Do Q\n"))


def page(xobjects):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(XObject=Dictionary(xobjects)),
            Contents=content,
        )
    )


pdf.pages.append(pikepdf.Page(page({"/FmList": fm_list, "/FmOuter": fm_outer})))
pdf.pages.append(pikepdf.Page(page({"/FmList": fm_shared, "/FmOuter": fm_outer})))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "imagereach_2b.pdf", min_version="1.7")
