#!/usr/bin/env python3
"""Build imagedraws_2b.pdf: images the XObject rules reach only through the
streams veraPDF parses beside a page's content, and the forms an annotation
appearance is.

veraPDF judges an image wherever a content stream it parses draws it: an
annotation's appearance, every glyph procedure of a Type 3 font text is shown
with, and a form that names the image through the page's resources. Each image
here carries /Interpolate true, so each reached image fails ISO 19005-2,
6.2.8 t3 once:

  IAP  drawn by the normal appearance of a Square annotation.
  IT3  drawn by the procedure of glyph b of the Type 3 font T3, which the page
       shows only glyph a of, in rendering mode 3.
  IPF  named by the page alone and drawn by the form FmBare, which carries no
       /Resources.
  BI   two inline images with /I true in the page's content, each an image.

The appearance streams are forms, and the form rules judge their dictionaries:

  ApOPI  the appearance of a second annotation, with no /Subtype and an /OPI
         key: ISO 19005-2, 6.2.9 t1.
  ApRef  the appearance of a third annotation, with a /Ref key: ISO 19005-2,
         6.2.9 t2.

And the image rules read an image by the role it is reached in:

  ImRef   a drawn image carrying /Ref, which no rule states for an image.
  Stencil a drawn image mask with /BitsPerComponent 2, which the value rule,
          t4, allows for any drawn image.
  Base    an image whose /Mask, BaseMask, declares /BitsPerComponent 8 and no
          /ImageMask: ISO 19005-2, 6.2.8 t5.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()


def image(sample, **extra):
    stream = Stream(pdf, sample)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = 1
    stream.Height = 1
    stream.ColorSpace = Name.DeviceGray
    stream.BitsPerComponent = 8
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


def form(body, resources=None, subtype=True, **extra):
    stream = Stream(pdf, body)
    if subtype:
        stream.Type = Name.XObject
        stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 100, 100])
    if resources is not None:
        stream.Resources = resources
    for key, value in extra.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


def annot(appearance, x):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Square,
            Rect=Array([x, 10, x + 40, 50]),
            F=4,
            AP=Dictionary(N=appearance),
        )
    )


DRAW = b"q 40 0 0 40 0 0 cm /Im Do Q"

i_ap = image(b"\x10", Interpolate=True)
i_t3 = image(b"\x20", Interpolate=True)
i_pf = image(b"\x30", Interpolate=True)

# The Type 3 font: glyph a paints a square, glyph b draws IT3. The page shows
# only a, in rendering mode 3, and veraPDF still parses every procedure.
glyph_a = pdf.make_indirect(Stream(pdf, b"1000 0 d0 0 0 1000 1000 re f"))
glyph_b = pdf.make_indirect(Stream(pdf, b"1000 0 d0 q 1000 0 0 1000 0 0 cm /Im Do Q"))
t3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=glyph_a, b=glyph_b),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a, Name.b])),
        FirstChar=97,
        LastChar=98,
        Widths=Array([1000, 1000]),
        Resources=Dictionary(XObject=Dictionary(Im=i_t3)),
    )
)

ap_image = form(DRAW, Dictionary(XObject=Dictionary(Im=i_ap)))
ap_opi = form(b"0 0 10 10 re f", Dictionary(), subtype=False, OPI=Dictionary())
ap_ref = form(b"0 0 10 10 re f", Dictionary(), Ref=Dictionary(F=String("other.pdf"), Page=0))
fm_bare = form(b"q 40 0 0 40 0 0 cm /IPF Do Q")

im_ref = image(b"\x40", Ref=Dictionary(F=String("other.pdf"), Page=0))
stencil = Stream(pdf, b"\x80")
stencil.Type = Name.XObject
stencil.Subtype = Name.Image
stencil.Width = 1
stencil.Height = 1
stencil.ImageMask = True
stencil.BitsPerComponent = 2
stencil = pdf.make_indirect(stencil)
base_mask = image(b"\x50")
base = image(b"\x60", Mask=base_mask)

INLINE = b"q 10 0 0 10 0 0 cm BI /W 1 /H 1 /CS /G /BPC 8 /I true ID \x70 EI Q"
content = (
    b"q 0.5 g 3 Tr BT /T3 12 Tf 10 700 Td (a) Tj ET Q\n"
    b"q /FmBare Do Q\n"
    + INLINE + b"\n" + INLINE + b"\n"
    b"q 40 0 0 40 100 100 cm /ImRef Do Q\n"
    b"q 0 g 40 0 0 40 200 100 cm /Stencil Do Q\n"
    b"q 40 0 0 40 300 100 cm /Base Do Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(T3=t3),
            XObject=Dictionary(FmBare=fm_bare, IPF=i_pf, ImRef=im_ref, Stencil=stencil, Base=base),
        ),
        Contents=pdf.make_indirect(Stream(pdf, content)),
        Annots=Array([annot(ap_image, 10), annot(ap_opi, 60), annot(ap_ref, 110)]),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "imagedraws_2b.pdf", min_version="1.7")
