#!/usr/bin/env python3
"""Build inlineinterp_2b.pdf: inline images that ask for interpolation in
every kind of stream veraPDF parses, for the repair that respells their
dictionaries in place.

ISO 19005-2, 6.2.8 t3 holds every image dictionary to an /Interpolate of
false, and an inline image writes the key /I (ISO 32000-1, 8.9.7, Table 93).
veraPDF judges each inline image as an image of its own, wherever a content
stream it parses holds it, so each image here fails 6.2.8 t3 once:

  Fm     a form XObject the page draws, whose image writes /I true.
  T3     the procedure of glyph a of a Type 3 font the page shows, whose image
         writes /Interpolate true.
  Ap     the normal appearance of a Square annotation, whose image writes
         /I true.
  page   the page's own content, whose image writes both /Interpolate true and
         /I true. The page's /Contents is a reference to an array of two
         streams, the image in the second.

Every image's data holds a NUL, a high byte, a delimiter and an end-of-line
marker, so a repair that moves a byte of it shows. The remediation rewrites
all four streams, and the page's through the array its /Contents names.

The first part of the page's content also shows text in F1, Helvetica, which
is not embedded: ISO 19005-2, 6.2.11.4.1 t1, which no repair answers. It stays
in every remediated output, so a rewrite of the page that a validator can no
longer read shows as a finding one tool reports and the other does not.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def inline(key):
    return b"q 10 0 0 10 0 0 cm BI /W 4 /H 1 /CS /G /BPC 8 " + key + b" ID \x00\xff)\r EI Q"


fm = Stream(pdf, inline(b"/I true"))
fm.Type = Name.XObject
fm.Subtype = Name.Form
fm.BBox = Array([0, 0, 100, 100])
fm.Resources = Dictionary()
fm = pdf.make_indirect(fm)

glyph_a = pdf.make_indirect(Stream(pdf, b"1000 0 d0 " + inline(b"/Interpolate true")))
t3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=glyph_a),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name.a])),
        FirstChar=97,
        LastChar=97,
        Widths=Array([1000]),
        Resources=Dictionary(),
    )
)

ap = Stream(pdf, inline(b"/I true"))
ap.Type = Name.XObject
ap.Subtype = Name.Form
ap.BBox = Array([0, 0, 40, 40])
ap.Resources = Dictionary()
ap = pdf.make_indirect(ap)
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([10, 10, 50, 50]),
        F=4,
        AP=Dictionary(N=ap),
    )
)

helvetica = pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.Helvetica))

part1 = pdf.make_indirect(
    Stream(
        pdf,
        b"q 100 0 0 100 100 400 cm /Fm Do Q\n"
        b"BT /T3 12 Tf 10 700 Td (a) Tj ET\n"
        b"BT /F1 12 Tf 10 650 Td (b) Tj ET\n",
    )
)
part2 = pdf.make_indirect(Stream(pdf, inline(b"/Interpolate true /I true") + b"\n"))
contents = pdf.make_indirect(Array([part1, part2]))

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(T3=t3, F1=helvetica), XObject=Dictionary(Fm=fm)),
        Contents=contents,
        Annots=Array([annot]),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "inlineinterp_2b.pdf", min_version="1.7")
