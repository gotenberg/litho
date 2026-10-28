#!/usr/bin/env python3
"""Build paintop_2b.pdf: transparency nothing paints, and halftones judged once.

ISO 19005-2, 6.2.10 t2 asks every page that contains transparency, in a
document without a PDF/A OutputIntent, for a blending colour space. veraPDF
decides that a page contains transparency at the operators that paint, under
the graphics state in force there: a fill tests /ca, a stroke /CA, both a soft
mask and a blend mode, and an alpha counts only below 1. The file declares no
OutputIntent and no page group, and every page paints in an ICCBased space,
so no page uses a device colour space of its own. Each of pages 1 to 7 sets a
transparent graphics state that no operator paints under:

| Page | Construct                                                 |
| ---- | --------------------------------------------------------- |
| 1    | `/ca 0.5` set inside `q ... Q`, the fill after `Q`         |
| 2    | `/ca 0.5` before a stroke alone                            |
| 3    | `/CA 0.5` before a fill alone                              |
| 4    | `/ca 1.5` before a fill                                    |
| 5    | a soft mask cleared by a graphics state without one        |
| 6    | a tiling pattern filling at `/ca 0.5`, selected, not used |
| 7    | an annotation appearance setting `/ca 0.5`, painting none  |

None of them contains transparency, so 6.2.10 t2 fires nowhere. litho counted
every graphics state a gs sets, and every tiling pattern the resources list,
and failed all seven.

Page 8 sets four graphics states whose halftones ISO 19005-2, 6.2.5 judges:

| State | /HT                                                         | Rule        |
| ----- | ----------------------------------------------------------- | ----------- |
| H1    | type 5, /Red and /Cyan naming one component, no transfer fn | silent      |
| H2    | type 5, /Blue and /Yellow naming one component              | silent      |
| H3    | the name /Foo                                               | `6.2.5/4`,5 |
| H4    | a type 6 halftone, a stream                                 | `6.2.5/4`   |

veraPDF judges a component written as an indirect object once, under the
colorant that reaches it first: it lists a type 5 halftone's components in the
order of the Java HashMap its dictionary is and walks them from the last, so
/Cyan and /Yellow, process colorants that need no transfer function, reach the
shared components before /Red and /Blue, which would. litho judged every
colorant, and failed t6 for /Red and /Blue. A halftone named by a name other
than /Default has no type and names itself, and a type 6 halftone is a
stream; litho read neither.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(colour_space, components, tag):
    """A structurally minimal ICC profile veraPDF will parse."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    body = bytes(header) + tag_table
    stream = Stream(pdf, body)
    stream.N = components
    return pdf.make_indirect(stream)


iccbased = pdf.make_indirect(Array([Name.ICCBased, icc_profile(b"RGB ", 3, b"ICCB")]))


def state(**entries):
    d = Dictionary(Type=Name.ExtGState)
    for key, value in entries.items():
        d[Name("/" + key)] = value
    return pdf.make_indirect(d)


soft_mask_group = Stream(pdf, b"/CS0 cs 1 1 1 scn 0 0 612 792 re f\n")
soft_mask_group.Type = Name.XObject
soft_mask_group.Subtype = Name.Form
soft_mask_group.BBox = Array([0, 0, 612, 792])
soft_mask_group.Resources = Dictionary(ColorSpace=Dictionary(CS0=iccbased))
soft_mask_group.Group = Dictionary(Type=Name.Group, S=Name.Transparency, CS=iccbased)

STATES = Dictionary(
    GSca=state(ca=0.5),
    GSCA=state(CA=0.5),
    GSca15=state(ca=1.5),
    GSsm=state(SMask=Dictionary(Type=Name.Mask, S=Name.Luminosity, G=pdf.make_indirect(soft_mask_group))),
    GSlw=state(LW=2),
)

pattern = Stream(pdf, b"/GSca gs /CS0 cs 0.2 0.4 0.6 scn 0 0 5 5 re f\n")
pattern.Type = Name.Pattern
pattern.PatternType = 1
pattern.PaintType = 1
pattern.TilingType = 1
pattern.BBox = Array([0, 0, 10, 10])
pattern.XStep = 10
pattern.YStep = 10
pattern.Resources = Dictionary(ExtGState=Dictionary(GSca=STATES.GSca), ColorSpace=Dictionary(CS0=iccbased))
pattern = pdf.make_indirect(pattern)

FILL = b"/CS0 cs 0.2 0.4 0.6 scn 60 60 200 200 re f\n"
STROKE = b"/CS0 CS 0.2 0.4 0.6 SCN 60 60 200 200 re S\n"


def add_page(content, annots=None, extgstate=None):
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            ExtGState=extgstate if extgstate is not None else STATES,
            ColorSpace=Dictionary(CS0=iccbased),
            Pattern=Dictionary(P1=pattern),
        ),
        Contents=Stream(pdf, content),
    )
    if annots is not None:
        page.Annots = annots
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))


add_page(b"q /GSca gs Q " + FILL)
add_page(b"/GSca gs " + STROKE)
add_page(b"/GSCA gs " + FILL)
add_page(b"/GSca15 gs " + FILL)
add_page(b"/GSsm gs /GSlw gs " + FILL)
add_page(b"/Pattern cs /P1 scn " + FILL)

appearance = Stream(pdf, b"/GSca gs\n")
appearance.Type = Name.XObject
appearance.Subtype = Name.Form
appearance.BBox = Array([0, 0, 100, 100])
appearance.Resources = Dictionary(ExtGState=Dictionary(GSca=STATES.GSca))
square = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([100, 100, 200, 200]),
        F=4,
        Contents=pikepdf.String("a square"),
        AP=Dictionary(N=pdf.make_indirect(appearance)),
    )
)
add_page(FILL, annots=Array([square]))

T1 = dict(Type=Name.Halftone, HalftoneType=1, Frequency=60, Angle=45, SpotFunction=Name.Round)


def halftone(**entries):
    return pdf.make_indirect(Dictionary(**entries))


red_cyan = halftone(**T1)
blue_yellow = halftone(**T1)
default = halftone(**T1)
type6 = Stream(pdf, b"\x00\x40\x80\xc0")
type6.Type = Name.Halftone
type6.HalftoneType = 6
type6.Width = 2
type6.Height = 2

HALFTONES = Dictionary(
    H1=state(HT=halftone(Type=Name.Halftone, HalftoneType=5, Red=red_cyan, Cyan=red_cyan, Default=default)),
    H2=state(HT=halftone(Type=Name.Halftone, HalftoneType=5, Blue=blue_yellow, Yellow=blue_yellow, Default=default)),
    H3=state(HT=Name.Foo),
    H4=state(HT=pdf.make_indirect(type6)),
)
add_page(b"/H1 gs /H2 gs /H3 gs /H4 gs " + FILL, extgstate=HALFTONES)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "paintop_2b.pdf", min_version="1.7")
