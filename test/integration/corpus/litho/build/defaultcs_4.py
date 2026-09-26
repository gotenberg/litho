#!/usr/bin/env python3
"""Build defaultcs_4.pdf: Default colour spaces judged in place of the family.

ISO 32000-1, 8.6.5.6 has the DefaultGray, DefaultRGB and DefaultCMYK entries of
a /ColorSpace resource dictionary replace the device family they name, so a
validator judges the Default space wherever a link reaches the family: its ICC
profile, its own device family, the alternate of a Separation. veraPDF reads
it as the colour space it is, and reads a Default it cannot parse, a number or
a reference to no object, as a resource name no dictionary defines, which ISO
19005-4, 6.2.2 test 3 fails once per content stream. Every corpus file that
sets a Default sets an ICCBased, CalRGB or CalGray space whose profile passes,
and none sets one veraPDF cannot read, so no count reached either reading.

Every page has resources of its own, so each row counts on its own. The ICC
profiles are abstract (`abst`), which ISO 19005-4, 6.2.4.2 t1 forbids, except
the control's monitor profile:

| Page | Default colour space                                     | Draws                          | Answers at PDF/A-4       |
| ---- | -------------------------------------------------------- | ------------------------------ | ------------------------ |
| 1    | DefaultRGB, a direct ICCBased array                      | `rg`, `f`                      | ICC x2                   |
| 2    | DefaultRGB, an indirect ICCBased array                   | `rg`, `f`, `rg`, `f`           | ICC x1                   |
| 3    | DefaultRGB `/DeviceCMYK`                                 | `rg`, `f`                      | DeviceCMYK x2            |
| 4    | DefaultCMYK, an indirect ICCBased CMYK array             | `k`, `f`, `gs` `/OPM 1`, `f`   | ICC x2, `6.2.4.2/2`      |
| 5    | DefaultCMYK, a DeviceN over DeviceCMYK                   | `k`, `f`                       | DeviceCMYK x2            |
| 6    | DefaultCMYK `[/CalCMYK ...]`, read as DeviceCMYK         | `k`, `f`                       | DeviceCMYK x2            |
| 7    | DefaultRGB `/Indexed` over DeviceGray, DefaultGray ICC   | `rg`, `f`                      | ICC x2                   |
| 8    | DefaultRGB a Separation over DeviceGray, DefaultGray ICC | `rg`, `f`                      | DeviceGray x2            |
| 9    | DefaultRGB `/CS0`, an entry holding DeviceGray           | `rg`, `f`                      | ICC x2                   |
| 10   | DefaultGray ICC, over the initial DeviceGray             | `f`, `f`                       | ICC x2                   |
| 11   | DefaultRGB ICC, an RGB image painted twice               | `Do`, `Do`                     | ICC x1                   |
| 12   | DefaultRGB ICC, a page group `/CS /DeviceRGB`            | nothing                        | ICC x1                   |
| 13   | none; a form's own DefaultRGB ICC                        | the form's `rg`, `f`           | ICC x2                   |
| 14   | DefaultRGB ICC; a form with no `/Resources`              | the form's `rg`, `f`           | DeviceRGB x2             |
| 15   | DefaultGray ICC; a form with empty resources             | the form's `f`                 | ICC x1                   |
| 16   | DefaultRGB over a monitor GRAY profile                   | `rg`, `f`                      | nothing                  |
| 17   | DefaultRGB `null`, which sets none                       | `cs`, `scn`, `f`               | DeviceRGB x3             |
| 18   | DefaultRGB `5`                                           | `rg`, `f`                      | t3, DeviceRGB x2         |
| 19   | DefaultRGB `99 0 R`, no such object                      | `/DeviceRGB cs`, `f`           | t3, DeviceGray x2        |
| 20   | DefaultGray `5`                                          | nothing                        | t3                       |
| 21   | an appearance stream's DefaultGray `5`                   | nothing                        | t3                       |
| 22   | a Type 3 font's DefaultRGB `5`, glyph after `d0`         | the glyph's `rg`, `f`          | t3, DeviceRGB x2, Gray   |
| 23   | a Type 3 font's DefaultRGB `5`, glyph past `d1`          | the glyph's `rg`, `f`          | Gray                     |
| 24   | a form's DefaultGray `5`, inheriting the page's fill     | the form's `f`                 | DeviceGray               |
| 25   | DefaultRGB `5`; a form with empty resources              | the form's `rg`, `f`           | DeviceRGB x2             |
| 26   | DefaultRGB `5`, an RGB image                             | `Do`                           | t3                       |
| 27   | none; a form's DefaultRGB `5`                            | the form's `rg`, then `n`      | t3, DeviceRGB            |

ICC is ISO 19005-4, 6.2.4.2 t1, t3 is 6.2.2 t3, and the device families are
6.2.4.3 t2 to t4. The DeviceGray on pages 22 and 23 is the text show itself,
painted in the page's initial DeviceGray.

The readings the rows rest on are veraPDF's, measured one construct per file
before they were packed. A direct Default is judged at every link and an
indirect one once for the document. A family the Default itself gives is not
replaced again (page 8's alternate), while the base of an `/Indexed` Default
is (page 7), and a name the Default gives reads as the entry it names (page 9).
A page's Default replaces no family a form, a tiling cell or an appearance
stream selects (page 14), but a form inherits the page's initial DeviceGray
and with it the page's DefaultGray (page 15). veraPDF checks no component
count, so page 16's gray profile under DefaultRGB passes. A Default it cannot
read leaves `rg` its family and makes `cs` a name no colour space answers,
which it judges as DeviceGray: page 18 fails DeviceRGB twice and page 19
DeviceGray twice at every PDF/A level, and t3 exists at PDF/A-4 alone. A glyph
past `d1` ignores its colour operators (ISO 32000-1, 9.6.5), and a form
inherits the colour its page selected.

qpdf drops a null dictionary value and cannot write a reference to a missing
object, so pages 17 and 19 carry placeholder names of the same length, patched
as bytes after the save, which moves no offset.

See ../README.md for the authoritative expected firing set.
"""

import pathlib
import struct

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_4 = 0x04200000
FILL = b"0 0 10 10 re f\n"
NULL_PLACEHOLDER = b"/LithoNullDefaultRGB"
DANGLING_PLACEHOLDER = b"/LithoDanglingDefault"


def profile(device_class, space, n):
    """A header-only ICC profile of the given class over the given space."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[8:12] = struct.pack(">I", ICC_VERSION_4)
    header[12:16] = device_class
    header[16:20] = space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = n
    return pdf.make_indirect(stream)


def icc(space, n, device_class=b"abst"):
    """A direct ICCBased array over a profile of its own."""
    return Array([Name.ICCBased, profile(device_class, space, n)])


def tint(outputs):
    """A type 2 tint transform onto a space of the given component count."""
    return Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0] * outputs), C1=Array([1] * outputs), N=1)


def rgb_image():
    image = Stream(pdf, b"\x00\x00\x00")
    image.Type = Name.XObject
    image.Subtype = Name.Image
    image.Width = 1
    image.Height = 1
    image.ColorSpace = Name.DeviceRGB
    image.BitsPerComponent = 8
    return pdf.make_indirect(image)


def form(body, resources=None):
    """A Form XObject; resources None leaves /Resources out altogether."""
    stream = Stream(pdf, body)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 50, 50])
    if resources is not None:
        stream.Resources = resources
    return pdf.make_indirect(stream)


def type3(glyph):
    """A Type 3 font whose resources hold a DefaultRGB veraPDF cannot read."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type3,
            FontBBox=Array([0, 0, 10, 10]),
            FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
            CharProcs=Dictionary(a=pdf.make_indirect(Stream(pdf, glyph))),
            Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name("/a")])),
            FirstChar=97,
            LastChar=97,
            Widths=Array([1000]),
            Resources=Dictionary(ColorSpace=Dictionary(DefaultRGB=5)),
        )
    )


def page(spaces=None, content=b"", **extra):
    """A page whose /ColorSpace resource holds spaces, painting content."""
    resources = Dictionary(extra.pop("resources", {}))
    if spaces is not None:
        resources.ColorSpace = Dictionary(spaces)
    d = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=resources,
        Contents=Stream(pdf, content),
    )
    for key, value in extra.items():
        d[Name("/" + key)] = value
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(d)))


RG = b"1 0 0 rg " + FILL
K = b"0 0 0 1 k " + FILL

page({"/DefaultRGB": icc(b"RGB ", 3)}, RG)
page({"/DefaultRGB": pdf.make_indirect(icc(b"RGB ", 3))}, RG + b"0 1 0 rg " + FILL)
page({"/DefaultRGB": Name.DeviceCMYK}, RG)
page(
    {"/DefaultCMYK": pdf.make_indirect(icc(b"CMYK", 4))},
    K + b"/GS1 gs " + FILL,
    resources={"/ExtGState": Dictionary(GS1=Dictionary(OPM=1, op=True))},
)
page({"/DefaultCMYK": Array([Name.DeviceN, Array([Name.Cyan, Name.Magenta, Name.Yellow, Name.Black]),
                             Name.DeviceCMYK, tint(1)])}, K)
page({"/DefaultCMYK": Array([Name("/CalCMYK"), Dictionary(WhitePoint=Array([0.9505, 1, 1.089]))])}, K)
page({"/DefaultRGB": Array([Name.Indexed, Name.DeviceGray, 1, String(b"\x00\xff")]),
      "/DefaultGray": icc(b"GRAY", 1)}, RG)
page({"/DefaultRGB": Array([Name.Separation, Name("/Spot"), Name.DeviceGray, tint(1)]),
      "/DefaultGray": icc(b"GRAY", 1)}, RG)
page({"/DefaultRGB": Name("/CS0"), "/CS0": Name.DeviceGray, "/DefaultGray": icc(b"GRAY", 1)}, RG)
page({"/DefaultGray": icc(b"GRAY", 1)}, FILL + FILL)
page({"/DefaultRGB": icc(b"RGB ", 3)}, b"/Im0 Do /Im0 Do\n", resources={"/XObject": Dictionary(Im0=rgb_image())})
page({"/DefaultRGB": icc(b"RGB ", 3)}, b"", Group=Dictionary(S=Name.Transparency, CS=Name.DeviceRGB))
page(None, b"/Fm0 Do\n", resources={"/XObject": Dictionary(
    Fm0=form(RG, Dictionary(ColorSpace=Dictionary(DefaultRGB=icc(b"RGB ", 3)))))})
page({"/DefaultRGB": icc(b"RGB ", 3)}, b"/Fm0 Do\n", resources={"/XObject": Dictionary(Fm0=form(RG))})
page({"/DefaultGray": icc(b"GRAY", 1)}, b"/Fm0 Do\n", resources={"/XObject": Dictionary(Fm0=form(FILL, Dictionary()))})
page({"/DefaultRGB": icc(b"GRAY", 1, b"mntr")}, RG)
page({"/DefaultRGB": Name(NULL_PLACEHOLDER.decode())}, b"/DeviceRGB cs 1 0 0 scn " + FILL)

page({"/DefaultRGB": 5}, RG)
page({"/DefaultRGB": Name(DANGLING_PLACEHOLDER.decode())}, b"/DeviceRGB cs " + FILL)
page({"/DefaultGray": 5}, b"0 0 10 10 re n\n")
appearance = form(b"0 0 10 10 re n\n", Dictionary(ColorSpace=Dictionary(DefaultGray=5)))
page(None, b"", Annots=Array([pdf.make_indirect(Dictionary(
    Type=Name.Annot, Subtype=Name.Square, Rect=Array([0, 0, 50, 50]), F=4, AP=Dictionary(N=appearance)))]))
page(None, b"BT /T3 12 Tf 10 10 Td (a) Tj ET\n",
     resources={"/Font": Dictionary(T3=type3(b"1000 0 d0 " + RG))})
page(None, b"BT /T3 12 Tf 10 10 Td (a) Tj ET\n",
     resources={"/Font": Dictionary(T3=type3(b"1000 0 0 0 10 10 d1 " + RG))})
page(None, b"/Fm0 Do\n", resources={"/XObject": Dictionary(
    Fm0=form(FILL, Dictionary(ColorSpace=Dictionary(DefaultGray=5))))})
page({"/DefaultRGB": 5}, b"/Fm0 Do\n", resources={"/XObject": Dictionary(Fm0=form(RG, Dictionary()))})
page({"/DefaultRGB": 5}, b"/Im0 Do\n", resources={"/XObject": Dictionary(Im0=rgb_image())})
page(None, b"/Fm0 Do\n", resources={"/XObject": Dictionary(
    Fm0=form(b"1 0 0 rg 0 0 10 10 re n\n", Dictionary(ColorSpace=Dictionary(DefaultRGB=5))))})

# Deliberately no /Metadata and no output intent.
save(pdf, "defaultcs_4.pdf", min_version="2.0")

path = pathlib.Path(out_path("defaultcs_4.pdf"))
raw = path.read_bytes()
for placeholder, value in ((NULL_PLACEHOLDER, b"null"), (DANGLING_PLACEHOLDER, b"99 0 R")):
    if raw.count(placeholder) != 1:
        raise SystemExit("defaultcs_4: expected exactly one %r" % placeholder)
    raw = raw.replace(placeholder, value.ljust(len(placeholder)))
path.write_bytes(raw)
print("patched the null and dangling Default colour spaces of", path)
