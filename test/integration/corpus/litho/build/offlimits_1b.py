#!/usr/bin/env python3
"""Build offlimits_1b.pdf: implementation limits in streams drawn colour-off.

ISO 32000-1, 8.7.3.3 has an uncoloured tiling pattern's cell ignore its colour
operators, and 9.6.5 a Type 3 glyph past its d1, and veraPDF reads every
stream such a cell or glyph draws the same way: it builds no colour space and
no pattern an operator there selects, so it judges neither the DeviceN
colourant limit, ISO 19005-1, 6.1.12 t9, on a space selected there, nor any
number given to a colour operator there against 6.1.12 t1. An image such a
stream paints is still built. A census found 17 files that draw a stream with
its colour operators off and none with a number past a limit in one, so no
count reached the reading, and no remediation had to leave such a number as
written.

Every construct sits on a page of its own. `DN` is a DeviceN of 33 colourants,
past the 8 ISO 19005-1, 6.1.12 allows and the 32 of ISO 19005-2, 6.1.13:

| Page | Construct                                                         | 6.1.12 at PDF/A-1b |
| ---- | ----------------------------------------------------------------- | ------------------ |
| 1    | an uncoloured cell paints an image in `DN`                        | t9 x1              |
| 2    | a coloured cell selects `DN`, beside an uncoloured cell           | nothing            |
| 3    | an uncoloured cell paints a form selecting `DN` and an image in it | t9 x1             |
| 4    | an `/Indexed` whose base names the `DN` entry, selected and filled | t9 x2             |
| 5    | an image whose `/Indexed` base names the `DN` entry               | t9 x1              |
| 6    | a glyph selects a pattern past its `d1`                           | nothing            |
| 7    | a glyph selects a pattern past the `Q` closing its `d1`           | t1 x1              |
| 8    | an uncoloured cell selects a pattern                              | nothing            |
| 9    | a form an uncoloured cell paints selects a pattern                | nothing            |
| 10   | a form painted in the Pattern space gives `scn` a pattern alone   | t1 x1              |
| 11   | `99999999999 g` in a form an uncoloured cell paints               | nothing            |
| 12   | `99999999999 g` in a glyph an uncoloured cell shows               | nothing            |
| 13   | `99999999999 g` in a coloured cell, beside an uncoloured one      | nothing            |
| 14   | `99999999999 g` in a form the page paints                         | t1 x1              |

On pages 6 to 10 the pattern selected is a coloured cell of its own giving
`w` the integer 2147483648, past 6.1.12 t1, which counts only where veraPDF
builds the pattern. Page 2 is the uncoloured pattern turning the colour
operators off for every tiling cell the same stream paints, the coloured one
beside it included; page 3 is an image being built where the space a colour
operator selects is not. Pages 11 to 14 are the numbers a content-limits
repair reads: it clamps the one compliance judges, on page 14, and leaves the
three it does not, since rewriting them changes the file for no rule.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

FILL = b"0 0 10 10 re f\n"
CELL_FILL = b"0 0 5 5 re f\n"
INDEX = String(b"\x00\x00\x00\xff\xff\xff")
BIG_W = b"2147483648 w "
BIG_G = b"99999999999 g "


def devicen(n=33):
    """A DeviceN of n spot colourants over DeviceCMYK."""
    return Array([
        Name.DeviceN,
        Array([Name("/S%d" % i) for i in range(n)]),
        Name.DeviceCMYK,
        Dictionary(FunctionType=4, Domain=Array([0, 1] * n), Range=Array([0, 1] * 4), Length=1),
    ])


def image(colour_space):
    img = Stream(pdf, b"\x00")
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 1
    img.Height = 1
    img.BitsPerComponent = 8
    img.ColorSpace = colour_space
    return pdf.make_indirect(img)


def form(body, resources):
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 10, 10])
    stream.Resources = resources
    return pdf.make_indirect(stream)


def tiling(paint, body, resources=None):
    stream = Stream(pdf, body)
    stream.Type = Name.Pattern
    stream.PatternType = 1
    stream.PaintType = paint
    stream.TilingType = 1
    stream.BBox = Array([0, 0, 10, 10])
    stream.XStep = 10
    stream.YStep = 10
    stream.Resources = resources if resources is not None else Dictionary()
    return pdf.make_indirect(stream)


def big_cell():
    """A coloured cell of its own whose w operand is past 6.1.12 t1."""
    return tiling(1, BIG_W + CELL_FILL)


def type3(glyph, resources):
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 1, 1]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(a=pdf.make_indirect(Stream(pdf, glyph))),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name("/a")])),
            FirstChar=97,
            LastChar=97,
            Widths=Array([1000]),
            Resources=resources,
        )
    )


def page(content, **resources):
    d = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary({"/" + k: v for k, v in resources.items()}),
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(d)))


OFF_SPACE = Dictionary(CSp=Array([Name.Pattern, Name.DeviceGray]))
OFF_FILL = b"/CSp cs 0.5 /P2 scn " + FILL
TEXT = b"BT /T3 12 Tf 10 10 Td (a) Tj ET\n"

page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(
    P2=tiling(2, b"/Im0 Do\n", Dictionary(XObject=Dictionary(Im0=image(devicen()))))))
page(b"/Pattern cs /P1 scn " + FILL + OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(
    P1=tiling(1, b"/CSn cs 1 scn " + CELL_FILL, Dictionary(ColorSpace=Dictionary(CSn=devicen()))),
    P2=tiling(2, CELL_FILL)))
page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(P2=tiling(2, b"/Fm0 Do\n", Dictionary(XObject=Dictionary(
    Fm0=form(b"/CSn cs 1 scn " + FILL + b"/Im0 Do\n",
             Dictionary(ColorSpace=Dictionary(CSn=devicen()), XObject=Dictionary(Im0=image(devicen())))))))))
page(b"/CS1 cs 0 sc " + FILL, ColorSpace=Dictionary(CS0=devicen(), CS1=Array([Name.Indexed, Name("/CS0"), 1, INDEX])))
page(b"/Im0 Do\n", ColorSpace=Dictionary(CS0=devicen()),
     XObject=Dictionary(Im0=image(Array([Name.Indexed, Name("/CS0"), 1, INDEX]))))

page(TEXT, Font=Dictionary(T3=type3(b"1000 0 0 0 1 1 d1 /Pattern cs /P1 scn 0 0 1 1 re f\n",
                                    Dictionary(Pattern=Dictionary(P1=big_cell())))))
page(TEXT, Font=Dictionary(T3=type3(b"q 1000 0 0 0 1 1 d1 Q /Pattern cs /P1 scn 0 0 1 1 re f\n",
                                    Dictionary(Pattern=Dictionary(P1=big_cell())))))
page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(
    P2=tiling(2, b"/Pattern cs /P1 scn " + CELL_FILL, Dictionary(Pattern=Dictionary(P1=big_cell())))))
page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(P2=tiling(2, b"/Fm0 Do\n", Dictionary(XObject=Dictionary(
    Fm0=form(b"/Pattern cs /P1 scn " + FILL, Dictionary(Pattern=Dictionary(P1=big_cell()))))))))
page(b"/Pattern cs /Fm0 Do\n", XObject=Dictionary(
    Fm0=form(b"/P1 scn " + FILL, Dictionary(Pattern=Dictionary(P1=big_cell())))))

page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(P2=tiling(2, b"/Fm0 Do\n", Dictionary(XObject=Dictionary(
    Fm0=form(BIG_G + FILL, Dictionary()))))))
page(OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(P2=tiling(2, TEXT, Dictionary(Font=Dictionary(
    T3=type3(b"1000 0 d0 " + BIG_G + b"0 0 1 1 re f\n", Dictionary()))))))
page(b"/Pattern cs /P1 scn " + FILL + OFF_FILL, ColorSpace=OFF_SPACE, Pattern=Dictionary(
    P1=tiling(1, BIG_G + CELL_FILL), P2=tiling(2, CELL_FILL)))
page(b"/Fm0 Do\n", XObject=Dictionary(Fm0=form(BIG_G + FILL, Dictionary())))

# Deliberately no /Metadata and no output intent.
save(pdf, "offlimits_1b.pdf", min_version="1.4")
