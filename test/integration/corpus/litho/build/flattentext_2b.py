#!/usr/bin/env python3
"""Build flattentext_2b.pdf: transparency over, under and on text, which
transparency flattening keeps as text, at PDF/A-2b.

Writer.FlattenTransparency composites each region of transparent content into
an opaque image and keeps every text object as text, drawn above the image
(the R4 decision). The corpora hold transparency, but no blend mode drawn over
text, no transparent text over content, no transparency group showing text,
and no text under a constant alpha of 0, so the flatten cell never saw what
flattening does with text. Every glyph is a Type 3 glyph whose description
paints in the text's colour, with a /ToUnicode map, so the text extracts and
draws the same in every renderer:

| Page | Draws                                                              |
| ---- | ------------------------------------------------------------------ |
| 1    | text at ca 0.5 and Multiply text on the bare page, and a square    |
|      | at ca 0.5 beside them                                              |
| 2    | a knockout group of two squares at ca 0.6 with text between them,  |
|      | and a non-isolated group holding an isolated one that shows text   |
|      | and a square under a luminosity soft mask                          |
| 3    | text under ca 0 over a grey page, text stroked under CA 0 and      |
|      | filled, and a square at ca 0.5                                     |
| 4    | a line of text, then a Multiply rectangle and a Screen rectangle   |
|      | over parts of it                                                   |
| 5    | text at ca 0.5 over a blue band, and text under a luminosity soft  |
|      | mask                                                               |

What flattening makes of each, by ISO 32000-2, 11: the text on page 1's bare
page is drawn exactly in its colour composited over white (Normal and Multiply
give the source colour over white, 11.3.5); page 2's groups are composited and
their text drawn above, kept hidden where the source hid it; page 3's
invisible text is drawn in rendering mode 3 and the stroked
text in mode 0, both exact. Pages 4 and 5 hold what flattening approximates and
reports: the text the Screen rectangle tinted draws above it untinted (the
Multiply one leaves black text black, which stays exact), the text over the
band and the soft-masked text draw opaque. The flatten cell renders the first three
pages; its text and transparency checks read all five.

The file declares no OutputIntent and its pages no /Group, so every page that
paints DeviceRGB fails ISO 19005-2, 6.2.4.3 t2 and every page that holds
transparency fails 6.2.10 t2.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, tounicode_cmap, type3_font, xmp_packet

pdf = pikepdf.new()

GLYPHS = [
    ("A", 600, "0 0 500 700 re f"),
    ("B", 600, "0 0 500 700 re 100 100 300 500 re f*"),
    ("C", 600, "0 0 m 500 0 l 250 700 l f"),
    ("D", 600, "0 300 500 150 re f 175 0 150 700 re f"),
    ("E", 600, "200 0 120 700 re f 0 580 500 120 re f"),
]
font = type3_font(pdf, GLYPHS, [600] * len(GLYPHS))
font.ToUnicode = tounicode_cmap(pdf, {65 + i: ord(name) for i, (name, _, _) in enumerate(GLYPHS)})
font = pdf.make_indirect(font)


def form(content, bbox, group=None, resources=None):
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array(bbox)
    if group is not None:
        stream.Group = group
    if resources is not None:
        stream.Resources = resources
    return pdf.make_indirect(stream)


def transparency(**attrs):
    return Dictionary(Type=Name.Group, S=Name.Transparency, **attrs)


def state(**entries):
    return pdf.make_indirect(Dictionary(Type=Name.ExtGState, **entries))


def add_page(content, resources):
    resources.Font = Dictionary(F=font)
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 100]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


ramp = Dictionary(
    ShadingType=2,
    ColorSpace=Name.DeviceGray,
    Coords=Array([0, 0, 200, 0]),
    Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0.2]), C1=Array([1]), N=1),
)
luminosity = form(b"/Sh sh\n", [0, 0, 200, 100], transparency(CS=Name.DeviceGray), Dictionary(Shading=Dictionary(Sh=ramp)))
soft = state(SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=luminosity))
half = state(ca=0.5, CA=0.5)

add_page(
    b"q /Half gs 1 0 0 rg BT /F 20 Tf 10 60 Td (ABCDE) Tj ET Q\n"
    b"q /MulT gs 0 0.6 0 rg BT /F 20 Tf 10 20 Td (ABC) Tj ET Q\n"
    b"q /Half gs 0 0 1 rg 140 20 40 60 re f Q\n",
    Dictionary(ExtGState=Dictionary(Half=half, MulT=state(BM=Name.Multiply, ca=0.7))),
)

text_resources = Dictionary(Font=Dictionary(F=font), ExtGState=Dictionary(G=state(ca=0.6), Soft=soft))
knockout = form(
    b"/G gs 1 0 0 rg 5 5 40 40 re f BT /F 16 Tf 0 g 8 20 Td (ABC) Tj ET /G gs 0 0 1 rg 25 25 40 40 re f\n",
    [0, 0, 80, 80],
    transparency(K=True),
    text_resources,
)
isolated = form(
    b"BT /F 16 Tf 0 0 0.5 rg 5 50 Td (DEA) Tj ET q /Soft gs 0 0.6 0 rg 0 0 80 45 re f Q\n",
    [0, 0, 80, 80],
    transparency(I=True),
    text_resources,
)
outer = form(b"0.9 0.9 0.5 rg 0 0 80 20 re f /I Do\n", [0, 0, 80, 80], transparency(), Dictionary(XObject=Dictionary(I=isolated)))
add_page(
    b"q 1 0 0 1 10 10 cm /K Do Q q 1 0 0 1 110 10 cm /O Do Q\n",
    Dictionary(XObject=Dictionary(K=knockout, O=outer)),
)

add_page(
    b"0.8 g 0 0 200 100 re f\n"
    b"q /Clear gs 0 g BT /F 20 Tf 10 60 Td (ABCDE) Tj ET Q\n"
    b"q /StrokeClear gs 2 Tr 1 0 0 rg 0 0 1 RG BT /F 20 Tf 10 20 Td (ABC) Tj ET Q\n"
    b"q /Half gs 0 0 1 rg 120 10 60 80 re f Q\n",
    Dictionary(ExtGState=Dictionary(Clear=state(ca=0), StrokeClear=state(CA=0), Half=half)),
)

add_page(
    b"BT /F 20 Tf 0 g 10 40 Td (ABCDEABCDE) Tj ET\n"
    b"q /Mul gs 1 1 0 rg 5 35 60 30 re f Q q /Scr gs 0 0 1 rg 70 35 60 30 re f Q\n",
    Dictionary(ExtGState=Dictionary(Mul=state(BM=Name.Multiply), Scr=state(BM=Name.Screen, ca=0.8))),
)

add_page(
    b"0 0 1 rg 0 0 200 35 re f q /Half gs 1 0 0 rg BT /F 20 Tf 10 10 Td (ABCDE) Tj ET Q\n"
    b"q /Soft gs 0 g BT /F 20 Tf 10 60 Td (DEABC) Tj ET Q\n",
    Dictionary(ExtGState=Dictionary(Half=half, Soft=soft)),
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "flattentext_2b.pdf", min_version="1.7")
