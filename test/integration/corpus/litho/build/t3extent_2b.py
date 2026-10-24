#!/usr/bin/env python3
"""Build t3extent_2b.pdf: Type 3 glyphs that paint far past their font's box, at PDF/A-2b.

A Type 3 glyph paints whatever its description paints (ISO 32000-1, 9.6.5):
/FontBBox is the producer's statement of the glyphs' bounds, and a d1 box
does not clip either. Redaction once bounded a glyph whose description it
could not bound, a shading or a form, by /FontBBox, so a region over the far
side of what the glyph painted selected nothing and left the paint standing
under it. No corpus file shows such a glyph painting past its font's box, so
no cell reached it.

The page, 400 by 400, shows three glyphs at Tf 10. Font T3 states a
/FontBBox of [0 0 1000 1000], a 10-point square at each origin:

| Glyph   | Opens with | Paints                                                     |
| ------- | ---------- | ---------------------------------------------------------- |
| T3 `A`  | d0         | a grey shading over its own 120-point square clip at 20 20 |
| T3 `B`  | d0         | a form whose 120 by 20 box, at 20 260, it fills            |
| T4 `A`  | d1         | a black bar 120 by 30 at 20 180, past its 10-point d1 box  |

T4 states a /FontBBox that holds the bar: poppler draws a d1 glyph into a
cache the size of that box, and clips what lies past it. The centred quarter
[100 100 300 300] meets all three paintings and none of the d1 box or the
box T3 states.

The RGB output intent covers DeviceGray, so the file is conforming at
PDF/A-2b.

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

shading = pdf.make_indirect(
    Dictionary(
        ShadingType=2,
        ColorSpace=Name.DeviceGray,
        Coords=Array([0, 0, 12000, 0]),
        Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0.2]), C1=Array([0.6]), N=1),
        Extend=Array([True, True]),
    )
)
form = Stream(pdf, b"0 g 0 0 12000 2000 re f\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 12000, 2000])
form.Resources = Dictionary()


def type3(bbox, procs, resources):
    """A Type 3 font of the glyphs procs, A onwards, stating bbox."""
    names = [Name("/" + chr(65 + i)) for i in range(len(procs))]
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array(bbox),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary({"/" + chr(65 + i): pdf.make_indirect(Stream(pdf, p)) for i, p in enumerate(procs)}),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([65] + names)),
            FirstChar=65,
            LastChar=64 + len(procs),
            Widths=Array([1000] * len(procs)),
            Resources=resources,
        )
    )


font = type3(
    [0, 0, 1000, 1000],
    [b"1000 0 d0\nq 0 0 12000 12000 re W n /Sh sh Q\n", b"1000 0 d0\n/Fm Do\n"],
    Dictionary(Shading=Dictionary(Sh=shading), XObject=Dictionary(Fm=pdf.make_indirect(form))),
)
# A renderer may draw a d1 glyph into a cache the size of /FontBBox (poppler
# does), so this font states a box that holds the bar its d1 box does not.
cached = type3([0, 0, 13000, 4000], [b"1000 0 0 0 1000 1000 d1\n0 0 12000 3000 re f\n"], Dictionary())

content = (
    b"0 g BT /T3 10 Tf\n"
    b"1 0 0 1 20 20 Tm (A) Tj\n"
    b"1 0 0 1 20 260 Tm (B) Tj\n"
    b"/T4 10 Tf 1 0 0 1 20 180 Tm (A) Tj\n"
    b"ET\n"
)
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(Font=Dictionary(T3=font, T4=cached)),
    Contents=pdf.make_indirect(Stream(pdf, content)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "t3extent_2b.pdf", min_version="1.7")
