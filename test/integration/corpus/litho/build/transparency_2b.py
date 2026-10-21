#!/usr/bin/env python3
"""Build transparency_2b.pdf: the transparency the renderer composites, at
PDF/A-2b.

ISO 32000-1, 11 composites every painting into the transparency group being
painted. The render goldens of the corpora held soft masks and blend modes,
but no knockout group, no luminosity mask with a /BC or a /TR, and no blend
mode drawn over the bare page, so the render cell never compared them. Each
page draws in DeviceRGB or DeviceGray:

| Page | Draws                                                               |
| ---- | ------------------------------------------------------------------- |
| 1    | four groups over a yellow band at ca 0.8: non-isolated, isolated,   |
|      | knockout, isolated knockout, each two squares Multiply at ca 0.6,   |
|      | and an isolated group holding a knockout group, composited Screen   |
| 2    | a luminosity mask over a gray ramp with /BC 0.5 outside its box,    |
|      | the same mask through an inverting /TR, and an alpha mask whose     |
|      | group holds an opaque square and one at ca 0.4                      |
| 3    | the sixteen blend modes, one per cell, over two backdrop colours    |
|      | and the bare page                                                   |

poppler 26.09.0 draws all three as ISO 32000-1 asks. It ignores the alpha
source flag (AIS), text knockout (TK) and a graphics-state soft mask over an
image that has its own /SMask, so the file holds none of those: litho's unit
tests check them against the specification.

The file declares no OutputIntent and its pages no /Group, so every page that
paints DeviceRGB fails ISO 19005-2, 6.2.4.3 t2 and every page that holds
transparency fails 6.2.10 t2.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


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


def add_page(width, height, content, resources):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, width, height]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


SQUARES = b"/G gs 1 0 0 rg 5 5 30 30 re f 0 0 1 rg 15 15 30 30 re f\n"
SQUARES_RESOURCES = Dictionary(ExtGState=Dictionary(G=Dictionary(Type=Name.ExtGState, ca=0.6, BM=Name.Multiply)))
groups = {
    "N": form(SQUARES, [0, 0, 50, 50], transparency(), SQUARES_RESOURCES),
    "I": form(SQUARES, [0, 0, 50, 50], transparency(I=True), SQUARES_RESOURCES),
    "K": form(SQUARES, [0, 0, 50, 50], transparency(K=True), SQUARES_RESOURCES),
    "IK": form(SQUARES, [0, 0, 50, 50], transparency(I=True, K=True), SQUARES_RESOURCES),
}
knockout = form(
    b"/D gs 1 0 0 rg 5 5 20 30 re f 0 1 1 rg 15 10 20 30 re f\n",
    [0, 0, 50, 50],
    transparency(K=True),
    Dictionary(ExtGState=Dictionary(D=Dictionary(Type=Name.ExtGState, ca=0.7, BM=Name.Difference))),
)
groups["O"] = form(
    b"0.5 g 2 2 46 15 re f /K Do\n",
    [0, 0, 50, 50],
    transparency(I=True),
    Dictionary(XObject=Dictionary(K=knockout)),
)
add_page(
    250,
    60,
    b"1 1 0 rg 0 0 250 25 re f /A gs\n"
    b"q 1 0 0 1 0 5 cm /N Do Q q 1 0 0 1 50 5 cm /I Do Q q 1 0 0 1 100 5 cm /K Do Q\n"
    b"q 1 0 0 1 150 5 cm /IK Do Q /S gs q 1 0 0 1 200 5 cm /O Do Q\n",
    Dictionary(
        XObject=Dictionary(**groups),
        ExtGState=Dictionary(
            A=Dictionary(Type=Name.ExtGState, ca=0.8),
            S=Dictionary(Type=Name.ExtGState, BM=Name.Screen, ca=0.9),
        ),
    ),
)

ramp = Dictionary(
    ShadingType=2,
    ColorSpace=Name.DeviceGray,
    Coords=Array([20, 0, 100, 0]),
    Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0]), C1=Array([1]), N=1),
)
luminosity = form(
    b"/Sh sh\n",
    [20, 0, 100, 100],
    transparency(CS=Name.DeviceGray),
    Dictionary(Shading=Dictionary(Sh=ramp)),
)
invert = pdf.make_indirect(Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([1]), C1=Array([0]), N=1))
alpha = form(
    b"0 g 110 10 40 80 re f /H gs 150 10 40 80 re f\n",
    [100, 0, 200, 100],
    transparency(),
    Dictionary(ExtGState=Dictionary(H=Dictionary(Type=Name.ExtGState, ca=0.4))),
)
add_page(
    200,
    100,
    b"0 0 1 rg 0 0 100 100 re f q /L gs 1 0 0 rg 0 50 100 50 re f Q q /T gs 0 1 0 rg 0 0 100 50 re f Q\n"
    b"1 1 0 rg 100 0 100 100 re f q /M gs 0 0 1 rg 100 0 100 100 re f Q\n",
    Dictionary(
        ExtGState=Dictionary(
            L=Dictionary(Type=Name.ExtGState, SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=luminosity, BC=Array([0.5]))),
            T=Dictionary(
                Type=Name.ExtGState,
                SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=luminosity, BC=Array([0.5]), TR=invert),
            ),
            M=Dictionary(Type=Name.ExtGState, SMask=Dictionary(Type=Name.Mask, S=Name.Alpha, G=alpha)),
        )
    ),
)

MODES = [
    "Normal", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn",
    "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity",
]
states = {}
content = b""
for i, mode in enumerate(MODES):
    states["B%d" % i] = Dictionary(Type=Name.ExtGState, BM=Name("/" + mode))
    x, y = (i % 4) * 40, (3 - i // 4) * 40
    content += b"0.9 0.5 0.1 rg %d %d 20 26 re f 0.2 0.4 0.8 rg %d %d 20 26 re f\n" % (x, y, x + 20, y)
    content += b"q /B%d gs 0.3 0.8 0.5 rg %d %d 30 32 re f Q\n" % (i, x + 5, y + 5)
add_page(160, 160, content, Dictionary(ExtGState=Dictionary(**states)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "transparency_2b.pdf", min_version="1.7")
