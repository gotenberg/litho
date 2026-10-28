#!/usr/bin/env python3
"""Build colouroff_2b.pdf: resource names a stream with its colour off never uses.

ISO 19005-2, 6.2.2 test 2 fails a content stream that resolves a resource name
only through the page's resources. A Type 3 glyph description past its `d1`
ignores its colour operators (ISO 32000-1, 9.6.5, Table 113), and so does the
cell of an uncoloured tiling pattern (8.7.3.3, Table 75). veraPDF processes
none of `cs`, `CS`, `sh`, `scn` or `SCN` there, so it resolves none of their
names and the stream uses no inherited name through them. No file of any
corpus selects a colour resource the page alone defines from such a stream.

Every stream below has no `/ColorSpace`, `/Shading` or `/Pattern` of its own,
so each name falls back to the page's:

| Stream                      | Names through the page                  | 6.2.2/2 |
| --------------------------- | --------------------------------------- | ------- |
| glyph of `T1`, past `d1`    | `/CS0 cs`                               | silent  |
| glyph of `T2`, past `d1`    | `/Sh0 sh`                               | silent  |
| glyph of `T3`, past `d1`    | `/Pattern cs /P0 scn`                   | silent  |
| glyph of `T4`, after `d0`   | `/CS0 cs`                               | fires   |
| cell of uncoloured `PUnc`   | `/CS0 cs`                               | silent  |
| cell of coloured `PCol`     | `/CS0 cs`                               | fires   |

Test 2 judges the stream, not the name, so each stream fails it once at most,
and the two controls make it fire twice. Every glyph sits in a font of its own:
veraPDF gives the glyphs of one Type 3 font a single set of inherited names.

A form paints `PUnc`, not the page. Every tiling cell a stream paints begins in
one state, which an uncoloured pattern turns off for all of them, so the page
painting `PUnc` beside `PCol` would silence the coloured control too. The form
carries its own resources, so it uses no inherited name itself.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

FILL = b"0 0 5 5 re f\n"
CALRGB = Array([Name.CalRGB, Dictionary(WhitePoint=Array([0.9505, 1, 1.089]))])


def tiling(paint_type, content):
    """A tiling pattern with empty resources, so its names fall back to the page."""
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = paint_type
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 10, 10])
    pat.XStep = 10
    pat.YStep = 10
    pat.Resources = Dictionary()
    return pdf.make_indirect(pat)


def type3(glyph):
    """A Type 3 font of one glyph, with empty resources."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 10, 10]),
            FontMatrix=Array([0.1, 0, 0, 0.1, 0, 0]),
            CharProcs=Dictionary(a=pdf.make_indirect(Stream(pdf, glyph))),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name("/a")])),
            FirstChar=97,
            LastChar=97,
            Widths=Array([10]),
            Resources=Dictionary(),
        )
    )


D1 = b"10 0 0 0 10 10 d1 "
shading = pdf.make_indirect(
    Dictionary(
        ShadingType=2,
        ColorSpace=CALRGB,
        Coords=Array([0, 0, 1, 0]),
        Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0]), C1=Array([1, 1, 1]), N=1),
    )
)
uncoloured = tiling(2, b"/CS0 cs 1 0 0 sc " + FILL)

form = Stream(pdf, b"/PS cs 0.5 /PUnc scn 0 0 10 10 re f\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 10, 10])
form.Resources = Dictionary(
    ColorSpace=Dictionary(PS=Array([Name.Pattern, Name.DeviceGray])),
    Pattern=Dictionary(PUnc=uncoloured),
)

resources = Dictionary(
    Font=Dictionary(
        T1=type3(D1 + b"/CS0 cs 1 0 0 sc " + FILL),
        T2=type3(D1 + b"/Sh0 sh\n"),
        T3=type3(D1 + b"/Pattern cs /P0 scn " + FILL),
        T4=type3(b"10 0 d0 /CS0 cs 1 0 0 sc " + FILL),
    ),
    ColorSpace=Dictionary(CS0=CALRGB),
    Shading=Dictionary(Sh0=shading),
    Pattern=Dictionary(P0=tiling(1, b"/CS0 cs 0 1 0 sc " + FILL), PCol=tiling(1, b"/CS0 cs 0 0 1 sc " + FILL)),
    XObject=Dictionary(Fm=pdf.make_indirect(form)),
)

content = (
    b"q /CS0 cs 0 0 0 sc\n"
    b"BT /T1 10 Tf 10 150 Td (a) Tj /T2 10 Tf (a) Tj /T3 10 Tf (a) Tj /T4 10 Tf (a) Tj ET\n"
    b"Q\n"
    b"q /Pattern cs /PCol scn 10 100 20 20 re f Q\n"
    b"q 1 0 0 1 50 100 cm /Fm Do Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "colouroff_2b.pdf", min_version="1.7")
