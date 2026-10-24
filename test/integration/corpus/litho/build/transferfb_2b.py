#!/usr/bin/env python3
"""Build transferfb_2b.pdf: a shading drawn under an inverting transfer function.

The raster fallback of a redaction (pdf.WithRedactionRasterFallback) draws a
shading paint the regions cut as an image of the page, which litho's renderer
composites with every graphics state in force applied, the transfer function
among them (ISO 32000-1, 10.5). The image is drawn where the shading was, so
the transfer function the content set is still in force there: applied a
second time, an inverting one turned the composite back into its negative,
and every pixel the image holds outside the regions showed inverted (pdfjs
issue20722, whose /TR inverts the whole page). No fixture here set a transfer
function under a unit the fallback rasterises, and issue20722 lies outside the
pdfjs render sample the redact-render cell reads.

One 400 by 400 page, its content in one graphics state that sets /TR to
f(x) = 1 - x (a Type 2 function, ISO 32000-1, 7.10.3):

| Paint                                      | Source colour | Shown as |
| ------------------------------------------ | ------------- | -------- |
| `rg` fill of the page                      | white         | black    |
| `rg` fill, `[50 50 350 350]`               | 0 0.5 1       | orange   |
| axial shading `sh`, clipped to the quarter | red to blue   | inverted |

The centred quarter of the page, [100 100 300 300], is the shading's clip, so
the redact-render cell's p1-quarter and p1-band regions cut it: the default
mode refuses the shading, and the fallback rasterises it.

/TR fails 6.2.5/1 at PDF/A-2b (ISO 19005-2, 6.2.5 forbids the key). See
../README.md for the authoritative expected firing set.
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

invert = Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([1]), C1=Array([0]), N=1)
shading = Dictionary(
    ShadingType=2,
    ColorSpace=Name.DeviceRGB,
    Coords=Array([100, 0, 300, 0]),
    Function=Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([1, 0, 0]), C1=Array([0, 0, 1]), N=1),
)
body = (
    b"/Inv gs 1 1 1 rg 0 0 400 400 re f\n"
    b"0 0.5 1 rg 50 50 300 300 re f\n"
    b"q 100 100 200 200 re W n /Sh0 sh Q\n"
)
page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 400, 400]),
    Resources=Dictionary(
        ExtGState=Dictionary(Inv=Dictionary(Type=Name.ExtGState, TR=invert)),
        Shading=Dictionary(Sh0=pdf.make_indirect(shading)),
    ),
    Contents=pdf.make_indirect(Stream(pdf, body)),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "transferfb_2b.pdf", min_version="1.7")
