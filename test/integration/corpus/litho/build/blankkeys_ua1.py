#!/usr/bin/env python3
"""Build blankkeys_ua1.pdf: images whose dictionaries say where and how they
paint, blanked by a redaction, at PDF/UA-1.

Redaction rewrites the image a region covers with new samples. The rewrite
wrote a fresh dictionary, so an image lost every entry blanking did not set,
and with /OC a hidden image's blank showed. Four 16 x 16 RGB ramps on page 1
carry the entries a blanked image keeps (ISO 32000-1, Table 89), and page 2
draws one of them again, so that one is blanked in a copy:

| Image | Draws on | Carries                                       | Tagged as                         |
| ----- | -------- | --------------------------------------------- | --------------------------------- |
| ImA   | 1        | /Interpolate true, /Intent /Perceptual, /Name | a Figure's marked content, MCID 0 |
| ImB   | 1        | /OC naming a group the default config hides   | an artifact                       |
| ImC   | 1        | /StructParent 1                               | a Figure's object reference       |
| ImE   | 1 and 2  | /StructParent 2                               | a Figure's object reference       |

ImC and ImE are drawn outside any marked content: their Figure elements name
them by /OBJR and their /StructParent keys map back to those elements (ISO
32000-1, 14.7.4.3 and 14.7.4.4). veraPDF reads such a Do as untagged content
(ISO 14289-1, 7.1 test 3), and so does litho.

See ../README.md for the authoritative expected firing set.
"""

import zlib

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

SIZE = 16


def ramp(shift, **entries):
    """A 16 x 16 DeviceRGB ramp, each sample row a different colour."""
    raw = bytes(
        v
        for y in range(SIZE)
        for x in range(SIZE)
        for v in ((x * 16 + shift) % 256, (y * 16) % 256, (255 - x * 16 + shift) % 256)
    )
    img = Stream(pdf, zlib.compress(raw))
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = SIZE
    img.Height = SIZE
    img.ColorSpace = Name.DeviceRGB
    img.BitsPerComponent = 8
    img.Filter = Name.FlateDecode
    for key, value in entries.items():
        img[Name("/" + key)] = value
    return pdf.make_indirect(img)


hidden = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Hidden layer")))
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([hidden]),
    D=Dictionary(Name=String("Default"), OFF=Array([hidden])),
)

im_a = ramp(0, Interpolate=True, Intent=Name.Perceptual, Name=Name.ImA)
im_b = ramp(64, OC=hidden)
im_c = ramp(128, StructParent=1)
im_e = ramp(192, StructParent=2)

PAGE1 = (
    b"/Figure <</MCID 0>> BDC q 160 0 0 160 40 150 cm /ImA Do Q EMC\n"
    b"/Artifact BMC q 160 0 0 160 210 150 cm /ImB Do Q EMC\n"
    b"q 80 0 0 80 60 60 cm /ImC Do Q\n"
    b"q 80 0 0 80 260 60 cm /ImE Do Q\n"
)
PAGE2 = b"q 160 0 0 160 150 150 cm /ImE Do Q\n"

pages = []
for i, (content, xobjects) in enumerate(
    [
        (PAGE1, Dictionary(ImA=im_a, ImB=im_b, ImC=im_c, ImE=im_e)),
        (PAGE2, Dictionary(ImE=im_e)),
    ]
):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 400, 400]),
            Resources=Dictionary(XObject=xobjects),
            Contents=pdf.make_indirect(Stream(pdf, content)),
            StructParents=i * 3,
        )
    )
    pdf.pages.append(pikepdf.Page(page))
    pages.append(page)

root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=root))


def figure(alt, kid):
    return pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.Figure, P=document, Pg=pages[0], Alt=String(alt), K=kid)
    )


fig_a = figure("A ramp, smoothed", 0)
fig_c = figure("A ramp drawn once", Dictionary(Type=Name.OBJR, Obj=im_c, Pg=pages[0]))
fig_e = figure("A ramp drawn on both pages", Dictionary(Type=Name.OBJR, Obj=im_e, Pg=pages[0]))
document.K = Array([fig_a, fig_c, fig_e])
root.K = document
root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([fig_a]), 1, fig_c, 2, fig_e, 3, Array([])]))
)
root.ParentTreeNextKey = 4

pdf.Root.StructTreeRoot = root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "blankkeys_ua1.pdf")
