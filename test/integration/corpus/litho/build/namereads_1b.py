#!/usr/bin/env python3
"""Build namereads_1b.pdf: the names veraPDF reads again at every operator.

ISO 19005-1, 6.1.12 test 4 bounds every name to 127 bytes. veraPDF judges a
name once as a COS object, and again each time a typed object of its model
wraps it in a new COS object: the colorant name of a Separation or a DeviceN at
every link an operator makes to the colour space, the /RI and /BM of a graphics
state at every gs that builds it, and the /Intent of an image once per image.
A colour space or a graphics state written directly has no identity of its
own, so it is read again at every link; one written as an indirect object is
read once for the whole document. No file of any corpus carries a name past the
limit in one of these places, so no count reached the re-reads.

Every long name below is distinct, so each row counts on its own:

| Name  | Where it sits                                  | Reached by            | 6.1.12/4 |
| ----- | ---------------------------------------------- | --------------------- | -------- |
| `A…`  | direct Separation, page `/ColorSpace`          | `cs`, `scn`, `f`      | 5        |
| `B…`  | indirect Separation, page `/ColorSpace`        | `cs`, `scn`, `f`      | 3        |
| `C…`  | colorant of a direct DeviceN                   | `cs`, `scn`, `f`      | 5        |
| `D…`  | Separation of a direct DeviceN's `/Colorants`  | `cs`, `scn`, `f`      | 4        |
| `E…`  | base of a direct `/Indexed` space              | `cs`, `f`             | 3        |
| `F…`  | direct Separation of an image painted twice    | the first `Do`        | 2        |
| `G…`  | `/Intent` of an image painted twice            | the first `Do`        | 2        |
| `H…`  | `/Intent` of that image's `/SMask`             | the first `Do`        | 2        |
| `I…`  | direct Separation of a form painted twice      | `cs`, `scn`, `f` once | 5        |
| `J…`  | `/BM` of a direct graphics state               | two `gs`              | 3        |
| `K…`  | `/RI` of an indirect graphics state            | two `gs`              | 2        |
| `L…`  | `/RI` of a direct graphics state               | one `gs`              | 2        |
| `M…`  | `/Intent` of an inline image                   | its `BI`              | 2        |
| `N…`  | direct Separation a glyph selects past `d1`    | nothing               | 2        |

The column counts the object sweep's one failure, the resource dictionary's
own reading of a Separation or DeviceN colorant (GFPDResources), and the
re-reads, 42 in all. `N…` is the control: a Type 3 glyph past its `d1` ignores
its colour operators (ISO 32000-1, 9.6.5), so veraPDF makes no link to the
Separation it selects.

Every construct also answers the clause it breaks on its own, a DeviceCMYK
alternate, an invalid rendering intent, a blend mode other than Normal, a soft
mask, and none of them is gated by what this file probes, so a checker that
stops at the first verdict reports one rule where several are due.

The graphics state whose `/RI` is written directly is set by one `gs` alone:
ISO 19005-1, 6.2.9 t1 judges the same `/RI`, and litho judges it once per
graphics state its resources list, where veraPDF judges it at every `gs`.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def long_name(letter):
    """A 130-byte name, past the 127 bytes ISO 19005-1, 6.1.12 t4 allows."""
    return Name("/" + letter * 130)


def tint(components):
    """A type 2 tint transform onto DeviceCMYK."""
    return Dictionary(
        FunctionType=2,
        Domain=Array([0, 1] * components),
        C0=Array([0, 0, 0, 0]),
        C1=Array([1, 0, 0, 0]),
        N=1,
    )


def separation(letter):
    """A Separation over DeviceCMYK naming the long colorant letter."""
    return Array([Name.Separation, long_name(letter), Name.DeviceCMYK, tint(1)])


def image(colour_space, **keys):
    """A one-pixel image XObject."""
    img = Stream(pdf, b"\x00")
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 1
    img.Height = 1
    img.ColorSpace = colour_space
    img.BitsPerComponent = 8
    for key, value in keys.items():
        img[Name("/" + key)] = value
    return pdf.make_indirect(img)


FILL = b"0 0 10 10 re f\n"

form = Stream(pdf, b"/SepI cs 1 scn " + FILL)
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 10, 10])
form.Resources = Dictionary(ColorSpace=Dictionary(SepI=separation("I")))

glyph = Stream(pdf, b"10 0 0 0 10 10 d1 /SepN cs 1 scn " + FILL)
type3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 10, 10]),
        FontMatrix=Array([0.1, 0, 0, 0.1, 0, 0]),
        CharProcs=Dictionary(a=pdf.make_indirect(glyph)),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name("/a")])),
        FirstChar=97,
        LastChar=97,
        Widths=Array([10]),
        Resources=Dictionary(),
    )
)

colorants = Array([Name.Cyan, Name("/Spot")])
resources = Dictionary(
    ColorSpace=Dictionary(
        SepA=separation("A"),
        SepB=pdf.make_indirect(separation("B")),
        DevC=Array([Name.DeviceN, Array([Name.Cyan, long_name("C")]), Name.DeviceCMYK, tint(2)]),
        DevD=Array(
            [
                Name.DeviceN,
                colorants,
                Name.DeviceCMYK,
                tint(2),
                Dictionary(Colorants=Dictionary(Spot=separation("D"))),
            ]
        ),
        IdxE=Array([Name.Indexed, separation("E"), 1, pikepdf.String(b"\x00\xff")]),
        SepN=separation("N"),
    ),
    XObject=Dictionary(
        ImF=image(separation("F")),
        ImG=image(Name.DeviceGray, Intent=long_name("G"), SMask=image(Name.DeviceGray, Intent=long_name("H"))),
        Fm=pdf.make_indirect(form),
    ),
    ExtGState=Dictionary(
        GJ=Dictionary(BM=long_name("J")),
        GK=pdf.make_indirect(Dictionary(RI=long_name("K"))),
        GL=Dictionary(RI=long_name("L")),
    ),
    Font=Dictionary(T3=type3),
)

content = (
    b"q /SepA cs 1 scn " + FILL + b"Q\n"
    b"q /SepB cs 1 scn " + FILL + b"Q\n"
    b"q /DevC cs 1 1 scn " + FILL + b"Q\n"
    b"q /DevD cs 1 1 scn " + FILL + b"Q\n"
    b"q /IdxE cs 0 sc " + FILL + b"Q\n"
    b"q /ImF Do /ImF Do Q\n"
    b"q /ImG Do /ImG Do Q\n"
    b"q /Fm Do /Fm Do Q\n"
    b"q /GJ gs /GJ gs /GK gs /GK gs /GL gs Q\n"
    b"q BI /W 1 /H 1 /CS /G /BPC 8 /Intent /" + b"M" * 130 + b" ID \x00 EI Q\n"
    b"q BT /T3 10 Tf (a) Tj ET Q\n"
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
save(pdf, "namereads_1b.pdf", min_version="1.4")
