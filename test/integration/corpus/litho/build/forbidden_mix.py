#!/usr/bin/env python3
"""Build complex_forbidden.pdf: a second deliberately complex fixture whose
violation mix is disjoint from megafont2b.pdf.

Targets the forbidden-construct families plus PostScript (Type 4) function tint
transforms on Separation / DeviceN, which is an open litho false-negative
cluster (veraPDF fires 6.2.4.4-t1 on such files, litho is silent).

Intended independent violation classes:
  A. LZWDecode filter                       (part 1 forbids it)
  B. embedded file / /EmbeddedFiles          (part 1 forbids)
  C. optional content /OCProperties          (part 1 forbids)
  D. /Launch action                          (forbidden action type)
  E. document-level JavaScript               (forbidden action type)
  F. /NeedsRendering true                    (forbidden)
  G. PostScript XObject (/Subtype /PS)       (forbidden)
  H. Separation with a Type 4 PS tint transform
  I. DeviceN with a Type 4 PS tint transform
  J. font not embedded
  K. annotation with /CA 0.5 (transparency) and no appearance stream
  L. no XMP metadata, no OutputIntent
"""

import zlib

import pikepdf
from _common import save
from pikepdf import Array, Dictionary, Name, Stream, String


pdf = pikepdf.new()


def lzw_compress(data: bytes) -> bytes:
    """Minimal LZW encoder matching the PDF/TIFF variable-code variant."""
    dict_size = 256
    table = {bytes([i]): i for i in range(dict_size)}
    next_code = 258
    code_len = 9
    out_bits = []

    def emit(code, width):
        for i in range(width - 1, -1, -1):
            out_bits.append((code >> i) & 1)

    emit(256, code_len)  # clear
    w = b""
    for ch in data:
        c = bytes([ch])
        if w + c in table:
            w = w + c
            continue
        emit(table[w], code_len)
        table[w + c] = next_code
        next_code += 1
        if next_code == (1 << code_len) + 1 and code_len < 12:
            code_len += 1
        w = c
    if w:
        emit(table[w], code_len)
    emit(257, code_len)  # EOD
    while len(out_bits) % 8:
        out_bits.append(0)
    return bytes(
        int("".join(str(b) for b in out_bits[i : i + 8]), 2)
        for i in range(0, len(out_bits), 8)
    )


# --- H/I: Type 4 (PostScript calculator) tint transforms.
ps_sep = Stream(pdf, b"{ dup 0.5 mul exch dup 0.3 mul exch 0.2 mul }")
ps_sep.FunctionType = 4
ps_sep.Domain = Array([0, 1])
ps_sep.Range = Array([0, 1, 0, 1, 0, 1])

sep_cs = Array([Name.Separation, Name("/Spot1"), Name.DeviceRGB, ps_sep])

ps_devn = Stream(pdf, b"{ 2 copy add 0.5 mul 3 1 roll pop pop dup dup }")
ps_devn.FunctionType = 4
ps_devn.Domain = Array([0, 1, 0, 1])
ps_devn.Range = Array([0, 1, 0, 1, 0, 1])

devn_cs = Array(
    [Name.DeviceN, Array([Name("/InkA"), Name("/InkB")]), Name.DeviceRGB, ps_devn]
)

# --- G: PostScript XObject.
psx = Stream(pdf, b"%!PS-Adobe-3.0\n0 0 moveto 10 10 lineto stroke\n")
psx.Type = Name.XObject
psx.Subtype = Name.PS

# --- A: an LZW-compressed image.
raw_img = bytes([0, 64, 128, 255] * 4)
lzw_img = Stream(pdf, lzw_compress(raw_img))
lzw_img.Type = Name.XObject
lzw_img.Subtype = Name.Image
lzw_img.Width = 4
lzw_img.Height = 4
lzw_img.ColorSpace = Name.DeviceGray
lzw_img.BitsPerComponent = 8
lzw_img.Filter = Name.LZWDecode

# --- J: a font with no embedded program.
font = Dictionary(
    Type=Name.Font,
    Subtype=Name.TrueType,
    BaseFont=Name("/Arial"),
    Encoding=Name.WinAnsiEncoding,
    FirstChar=65,
    LastChar=67,
    Widths=Array([722, 722, 722]),
    FontDescriptor=Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/Arial"),
        Flags=32,
        FontBBox=Array([-166, -225, 1000, 931]),
        ItalicAngle=0,
        Ascent=718,
        Descent=-207,
        CapHeight=718,
        StemV=88,
    ),
)

# --- C: an optional content group, referenced from a BDC in the content.
ocg = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Layer 1")))

content = b"""q
/CS0 cs 0.7 sc
50 700 100 40 re f
/CS1 cs 0.4 0.6 scn
50 650 100 40 re f
BT /F1 12 Tf 50 600 Td (ABC) Tj ET
q 60 0 0 60 50 500 cm /ImLZW Do Q
/OC /OC1 BDC
q 1 0 0 1 50 420 cm /PSX Do Q
EMC
Q
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(F1=pdf.make_indirect(font)),
        ColorSpace=Dictionary(CS0=sep_cs, CS1=devn_cs),
        XObject=Dictionary(ImLZW=lzw_img, PSX=psx),
        Properties=Dictionary(OC1=ocg),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# --- K: transparent annotation with no appearance stream.
annot = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Square,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    CA=0.5,
    IC=Array([1, 0, 0]),
)
pdf.pages[0].Annots = Array([pdf.make_indirect(annot)])

# --- B: an embedded file, reachable from the catalog name tree.
ef = Stream(pdf, b"attachment payload")
ef.Type = Name.EmbeddedFile
filespec = pdf.make_indirect(
    Dictionary(
        Type=Name.Filespec,
        F=String("data.txt"),
        UF=String("data.txt"),
        EF=Dictionary(F=ef),
    )
)

# --- D/E: forbidden actions.
launch = pdf.make_indirect(
    Dictionary(Type=Name.Action, S=Name.Launch, F=String("evil.exe"))
)
js = pdf.make_indirect(
    Dictionary(Type=Name.Action, S=Name.JavaScript, JS=String("app.alert('hi');"))
)

pdf.Root.Names = Dictionary(
    EmbeddedFiles=Dictionary(Names=Array([String("data.txt"), filespec])),
    JavaScript=Dictionary(Names=Array([String("boot"), js])),
)
pdf.Root.OpenAction = launch
pdf.Root.OCProperties = Dictionary(OCGs=Array([ocg]), D=Dictionary(ON=Array([ocg])))
pdf.Root.NeedsRendering = True

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "forbidden_mix.pdf")
