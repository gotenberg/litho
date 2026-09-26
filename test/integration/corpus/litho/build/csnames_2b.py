#!/usr/bin/env python3
"""Build csnames_2b.pdf: colour-space names inside colour space arrays.

ISO 32000-1, 8.6.3 lets a colour space array name its base, its alternate or a
colorant's space either by a family name or, as veraPDF reads it, by the key of
another entry of the same /ColorSpace resource dictionary. veraPDF resolves the
name against the one dictionary the space was read from, with no fallback to
the page's, and judges the entry it names in its place. No file of any corpus
names a /ColorSpace entry inside a colour space array, in a /Colorants
dictionary, as a shading's colour space or as an image's /Indexed base, so no
count reached the reading; a validator that took such a name for a space it
cannot read judged DeviceGray instead.

Every page has resources of its own, and every Separation names a colorant of
its own, so no two pages share a verdict:

| Page | Construct                                                         | Answers at PDF/A-2b       |
| ---- | ----------------------------------------------------------------- | ------------------------- |
| 1    | `[/Indexed /CS0 ...]`, `/CS0` being `/DeviceRGB`                  | DeviceRGB x2              |
| 2    | the same over `/CS0 [/DeviceRGB]`, a one-element array            | DeviceRGB x2              |
| 3    | `/CS2 /CS0`, an entry naming another                              | DeviceRGB x2              |
| 4    | a Separation whose alternate is `/CS0`, `/DeviceCMYK`             | DeviceCMYK x3             |
| 5    | a DeviceN whose alternate is `/CS0`, `/DeviceCMYK`                | DeviceCMYK x3             |
| 6    | a DeviceN over DeviceGray whose `/Colorants` names a Separation entry over DeviceRGB | DeviceRGB x3, DeviceGray x3 |
| 7    | DefaultRGB `[/Indexed /CS0 ...]`, `/CS0` being `/DeviceCMYK`      | DeviceCMYK x2             |
| 8    | an inline image `[/Indexed /CS0 ...]`                             | DeviceRGB x1              |
| 9    | `[/Indexed /CS0 ...]` over a direct abstract ICCBased entry       | ICC x2                    |
| 10   | the same over an indirect ICCBased entry                          | ICC x1                    |
| 11   | `/CSp /CSq`, `/CSq [/Pattern]`, painting a coloured tiling cell   | DeviceRGB x2, integer x1  |
| 12   | `/CSp /CSq`, `/CSq [/Pattern /DeviceRGB]`, an uncoloured cell     | DeviceRGB x1              |
| 13   | `[/Indexed 9 0 R ...]`, an indirect object holding `/DeviceRGB`   | DeviceRGB x2              |
| 14   | a form's `/Indexed` over `/CS0`, which only the page defines      | DeviceGray x2             |
| 15   | the page's `/Indexed` over `/CS0`, a form redefining `/CS0`       | DeviceRGB x2              |
| 16   | the page's `/Indexed` image a form paints, the form redefining `/CS0` | DeviceRGB x1          |
| 17   | a form's shading naming `/CS0`, which only the page defines       | nothing                   |
| 18   | the page's DeviceRGB shading a form paints, under the page's DefaultRGB | ICC x1              |
| 19   | the page's `/Indexed` DeviceRGB image a form paints, the same     | ICC x1                    |
| 20   | two Separations of one colorant over `/CS0` and `/DeviceCMYK`     | DeviceCMYK x6             |

ICC is ISO 19005-2, 6.2.4.2 t1 over an abstract profile, the device families
6.2.4.3 t2 to t4, and the integer the 2147483648 given to `w` in page 11's cell,
6.1.13 t1, which veraPDF reads only because the name reaches the Pattern space
and the pattern is painted. Page 17 is a control: the form's shading resolves
`/CS0` in the form's resources, where it names nothing, and veraPDF judges no
colour space. Page 20 is another: ISO 19005-2, 6.2.4.4 t2 compares the two
Separations' alternates as the spaces they name, the same DeviceCMYK, so the
test stays silent. Pages 15, 16, 18 and 19 also fail 6.2.2 t2, once each: the
form selects a colour space, an image or a shading only the page defines. Page
14's form does not: `/CS0` inside its own `/Indexed` is no resource name the
form looks up, and it reads as no colour space, which veraPDF judges as
DeviceGray.

Two readings stay out, because veraPDF cannot finish them: a name that reaches
itself again, which runs it out of time or stack, and a fan-out of names wide
enough to need litho's bound on the names one link follows. The page group
naming an entry is already in `blendcs_4.pdf`, and an entry holding an
indirect family name in govdocs thread_300_300760.pdf.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_4 = 0x04200000
FILL = b"0 0 10 10 re f\n"
INDEX = String(b"\x00\x00\x00\xff\xff\xff")


def abstract_rgb():
    """A header-only abstract profile over RGB, which 6.2.4.2 t1 forbids."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_4)
    header[12:16] = b"abst"
    header[16:20] = b"RGB "
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = 3
    return pdf.make_indirect(stream)


def tint(outputs):
    """A type 2 tint transform onto a space of the given component count."""
    return Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0] * outputs),
                      C1=Array([1] + [0] * (outputs - 1)), N=1)


def indexed(base):
    return Array([Name.Indexed, base, 1, INDEX])


def image(colour_space, mask=False):
    """A one-pixel image XObject, or an image mask."""
    img = Stream(pdf, b"\x00")
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 1
    img.Height = 1
    if mask:
        img.ImageMask = True
        img.BitsPerComponent = 1
    else:
        img.ColorSpace = colour_space
        img.BitsPerComponent = 8
    return pdf.make_indirect(img)


def form(body, resources):
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 10, 10])
    stream.Resources = resources
    return pdf.make_indirect(stream)


def tiling(paint, body, resources):
    stream = Stream(pdf, body)
    stream.Type = Name.Pattern
    stream.PatternType = 1
    stream.PaintType = paint
    stream.TilingType = 1
    stream.BBox = Array([0, 0, 10, 10])
    stream.XStep = 10
    stream.YStep = 10
    stream.Resources = resources
    return pdf.make_indirect(stream)


def shading(colour_space):
    return Dictionary(ShadingType=2, ColorSpace=colour_space, Coords=Array([0, 0, 10, 0]), Function=tint(3))


def page(content, **resources):
    d = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary({"/" + k: v for k, v in resources.items()}),
        Contents=Stream(pdf, content),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(d)))


def spaces(**entries):
    return Dictionary({"/" + k: v for k, v in entries.items()})


INDEXED_FILL = b"/CS1 cs 0 sc " + FILL

page(INDEXED_FILL, ColorSpace=spaces(CS0=Name.DeviceRGB, CS1=indexed(Name("/CS0"))))
page(INDEXED_FILL, ColorSpace=spaces(CS0=Array([Name.DeviceRGB]), CS1=indexed(Name("/CS0"))))
page(b"/CS2 cs 1 0 0 sc " + FILL, ColorSpace=spaces(CS0=Name.DeviceRGB, CS2=Name("/CS0")))
page(b"/CS1 cs 1 scn " + FILL, ColorSpace=spaces(
    CS0=Name.DeviceCMYK, CS1=Array([Name.Separation, Name("/SpotSep"), Name("/CS0"), tint(4)])))
page(b"/CS1 cs 1 scn " + FILL, ColorSpace=spaces(
    CS0=Name.DeviceCMYK, CS1=Array([Name.DeviceN, Array([Name.Cyan]), Name("/CS0"), tint(4)])))
page(b"/CS1 cs 1 scn " + FILL, ColorSpace=spaces(
    CS3=Array([Name.Separation, Name("/SpotCol"), Name.DeviceRGB, tint(3)]),
    CS1=Array([Name.DeviceN, Array([Name("/SpotCol")]), Name.DeviceGray, tint(4),
               Dictionary(Colorants=Dictionary(SpotCol=Name("/CS3")))]),
))
page(b"1 0 0 rg " + FILL, ColorSpace=spaces(CS0=Name.DeviceCMYK, DefaultRGB=indexed(Name("/CS0"))))
page(b"BI /W 1 /H 1 /BPC 8 /CS [/Indexed /CS0 1 <000000FFFFFF>] ID \x00 EI\n", ColorSpace=spaces(CS0=Name.DeviceRGB))
page(INDEXED_FILL, ColorSpace=spaces(CS0=Array([Name.ICCBased, abstract_rgb()]), CS1=indexed(Name("/CS0"))))
page(INDEXED_FILL, ColorSpace=spaces(
    CS0=pdf.make_indirect(Array([Name.ICCBased, abstract_rgb()])), CS1=indexed(Name("/CS0"))))
page(b"/CSp cs /P1 scn " + FILL,
     ColorSpace=spaces(CSq=Array([Name.Pattern]), CSp=Name("/CSq")),
     Pattern=Dictionary(P1=tiling(1, b"2147483648 w 1 0 0 rg 0 0 5 5 re f\n", Dictionary())))
page(b"/CSp cs 1 0 0 /P2 scn " + FILL,
     ColorSpace=spaces(CSq=Array([Name.Pattern, Name.DeviceRGB]), CSp=Name("/CSq")),
     Pattern=Dictionary(P2=tiling(2, b"/Im0 Do\n", Dictionary(XObject=Dictionary(Im0=image(None, mask=True))))))
page(INDEXED_FILL, ColorSpace=spaces(CS1=indexed(pdf.make_indirect(Name.DeviceRGB))))

page(b"/Fm0 Do\n", ColorSpace=spaces(CS0=Name.DeviceRGB), XObject=Dictionary(
    Fm0=form(INDEXED_FILL, Dictionary(ColorSpace=spaces(CS1=indexed(Name("/CS0")))))))
page(b"/Fm0 Do\n", ColorSpace=spaces(CS0=Name.DeviceRGB, CS1=indexed(Name("/CS0"))), XObject=Dictionary(
    Fm0=form(INDEXED_FILL, Dictionary(ColorSpace=spaces(CS0=Name.DeviceCMYK)))))
page(b"/Fm0 Do\n", ColorSpace=spaces(CS0=Name.DeviceRGB), XObject=Dictionary(
    Im0=image(indexed(Name("/CS0"))),
    Fm0=form(b"/Im0 Do\n", Dictionary(ColorSpace=spaces(CS0=Name.DeviceCMYK)))))
page(b"/Fm0 Do\n", ColorSpace=spaces(CS0=Name.DeviceRGB), XObject=Dictionary(
    Fm0=form(b"/Sh0 sh\n", Dictionary(Shading=Dictionary(Sh0=shading(Name("/CS0")))))))
page(b"/Fm0 Do\n", ColorSpace=spaces(DefaultRGB=Array([Name.ICCBased, abstract_rgb()])),
     Shading=Dictionary(Sh0=shading(Name.DeviceRGB)),
     XObject=Dictionary(Fm0=form(b"/Sh0 sh\n", Dictionary(ExtGState=Dictionary()))))
page(b"/Fm0 Do\n", ColorSpace=spaces(DefaultRGB=Array([Name.ICCBased, abstract_rgb()])),
     XObject=Dictionary(Im0=image(indexed(Name.DeviceRGB)),
                        Fm0=form(b"/Im0 Do\n", Dictionary(ExtGState=Dictionary()))))
shared_tint = pdf.make_indirect(tint(4))
page(b"/CS1 cs 1 scn " + FILL + b"/CS2 cs 1 scn " + FILL, ColorSpace=spaces(
    CS0=Name.DeviceCMYK,
    CS1=Array([Name.Separation, Name("/SpotTwin"), Name("/CS0"), shared_tint]),
    CS2=Array([Name.Separation, Name("/SpotTwin"), Name.DeviceCMYK, shared_tint]),
))

# Deliberately no /Metadata and no output intent.
save(pdf, "csnames_2b.pdf")
