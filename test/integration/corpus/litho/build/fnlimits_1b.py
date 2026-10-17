#!/usr/bin/env python3
"""Build fnlimits_1b.pdf: PostScript calculator functions past the
implementation limits.

ISO 19005-1, 6.1.12 and ISO 19005-2, 6.1.13 limit every number a conforming
file contains, and a type 4 function writes its program as a stream of numbers
(ISO 32000-1, 7.10.5). veraPDF parses the program of every type 4 function its
model reaches and judges each number as a CosInteger or a CosReal: the integer
limit (t1) and, at PDF/A-1, the real limit of 32767 (t2). Its model reaches a
function as the tint transform of a Separation or DeviceN space a link reaches,
through a graphics state's /TR or /TR2 and a halftone's /TransferFunction, and
through the /Functions of a stitching function, but never as a shading's
/Function. One page draws:

| Function | Reached as                                                 | Program numbers         | Verdict at PDF/A-1b |
| -------- | ---------------------------------------------------------- | ----------------------- | ------------------- |
| A        | tint transform of the Separation /Spot cs selects          | 2147483648 and 40000.5  | t1 and t2           |
| B        | /TransferFunction of the /Spot component of a type 5 /HT   | 99999.5                 | t2                  |
| E        | inside the stitching tint transform of the Separation /Two | 4294967296              | t1                  |
| C        | /Function of the shading sh paints                         | 2147483648 and 40000.5  | silent              |
| F        | tint transform of a Separation the resources list, unused  | 2147483648 and 40000.5  | silent              |

The stitching tint transform of /Two is a direct dictionary naming E, so every
link to /Two compares it with itself, which ISO 19005-2, 6.2.4.4 t2 must find
consistent. No corpus file holds a type 4 function past a limit, nor a direct
tint transform naming another object.

Every count was confirmed with live veraPDF 1.30.2. See ../README.md for the
authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def type4(program):
    """An indirect type 4 function of one input and one output."""
    return pdf.make_indirect(Stream(pdf, program, FunctionType=4, Domain=Array([0, 1]), Range=Array([0, 1])))


fn_a = type4(b"{ 2147483648 pop 40000.5 pop }")
fn_b = type4(b"{ pop 99999.5 pop 0.5 }")
fn_c = type4(b"{ 2147483648 pop 40000.5 pop }")
fn_e = type4(b"{ pop 4294967296 pop 0.5 }")
fn_f = type4(b"{ 2147483648 pop 40000.5 pop }")

stitching = Dictionary(FunctionType=3, Domain=Array([0, 1]), Functions=Array([fn_e]), Bounds=Array([]),
                       Encode=Array([0, 1]))
screen = Dictionary(Type=Name.Halftone, HalftoneType=1, Frequency=60, Angle=45, SpotFunction=Name.Round)
halftone = Dictionary(
    Type=Name.Halftone,
    HalftoneType=5,
    Default=screen,
    Spot=Dictionary(Type=Name.Halftone, HalftoneType=1, Frequency=60, Angle=45, SpotFunction=Name.Round,
                    TransferFunction=fn_b),
)
resources = Dictionary(
    ColorSpace=Dictionary(
        CS1=Array([Name.Separation, Name("/Spot"), Name.DeviceGray, fn_a]),
        CS2=Array([Name.Separation, Name("/Two"), Name.DeviceGray, stitching]),
        CS3=Array([Name.Separation, Name("/Unused"), Name.DeviceGray, fn_f]),
    ),
    ExtGState=Dictionary(GS1=Dictionary(Type=Name.ExtGState, HT=halftone)),
    Shading=Dictionary(Sh1=Dictionary(ShadingType=2, ColorSpace=Name.DeviceGray, Coords=Array([0, 0, 100, 0]),
                                      Function=fn_c)),
)
content = (b"/GS1 gs\n/CS1 cs 1 scn 10 10 50 50 re f\n/CS2 cs 1 scn 70 10 50 50 re f\n"
           b"q 130 10 50 50 re W n /Sh1 sh Q\n")
page = pdf.make_indirect(Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 200, 100]), Resources=resources,
                                    Contents=Stream(pdf, content)))
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))
save(pdf, "fnlimits_1b.pdf", min_version="1.4")
