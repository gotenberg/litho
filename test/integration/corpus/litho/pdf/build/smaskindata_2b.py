#!/usr/bin/env python3
"""Build smaskindata_2b.pdf: JPEG 2000 images that carry their soft mask in
their data, at PDF/A-2b.

ISO 32000-1, 8.9.5, Table 89 lets a JPXDecode image say, by /SMaskInData,
that the opacity channel of its JPEG 2000 data is its soft mask: 1 for
straight colour, 2 for colour preblended with the background, which JPEG 2000
premultiplies against zero (ITU-T T.800, Table I.16). The only such images in
the corpora are three pdfjs files, all value 1, none in the render sample, so
the render cell never drew one. The page shows two 32 x 32 RGBA images over
yellow and blue bands, each a gradient whose opacity falls from 255 at the
centre to 0 at a radius of 14 pixels:

| Image        | Data                                     | Entries                       |
| ------------ | ---------------------------------------- | ----------------------------- |
| `ImStraight` | straight colour, opacity channel (cdef 1) | `/SMaskInData 1`, sRGB `colr` |
| `ImPremul`   | colour premultiplied, cdef type 2         | `/SMaskInData 2`, `/DeviceRGB` |

Both decode, composited over the bands, to the same picture: a disc whose
edge shows the bands through it. poppler 26.09.0 and PDFium ignore
/SMaskInData and paint both as opaque squares, black outside the disc, which
the README adjudicates.

The codestreams are embedded, produced once with OpenJPEG 2.5 from planar raw
data (`opj_compress -F 32,32,4,8,u -r 1`, lossless), the COM marker that
carries the encoder's version stripped. The JP2 boxes are written here.

The file declares no OutputIntent and its page no /Group: it paints
DeviceRGB, which fails ISO 19005-2, 6.2.4.3, and holds transparency.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import base64
import struct

import _jp2
from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

SIZE = 32

STRAIGHT = base64.b64decode(
    "/0//UQAyAAAAAAAgAAAAIAAAAAAAAAAAAAAAIAAAACAAAAAAAAAAAAAEBwEBBwEBBwEBBwEB/1IA"
    "DAAAAAEBBQQEAAH/XAATQEBISFBISFBISFBISFBISFD/kAAKAAAAAAKbAAH/k8PnAgfPtAgC38Pn"
    "BAV/z7QIBcnD6gKH1AQEDwa/p+AEBvfPwAgEP8faBR9oDH4AQANfAwHDwfOEg+cJACINAc0/DBe4"
    "vwXfo+0EAAwXyZ/H2gYADQIRz8AWfgCw+0FAEIZEbLMQhod4Xw+vqJ2/wPkDwPkDQAjAIhnfnAdM"
    "vwDP0QrLsxp9yaH1AwAAz9VdLh/D6gYAIhoIYv5/x9ohH2iEH1CAIxLdQR3uAaCiDC6mjVc9XR+y"
    "n7Z7/jBF2UeYKBVpvX8kMpAQIFL/WmOHPKYaoSJ/wHwkwHwkQAkANqGamcOCSCgXPmKif8scWz8/"
    "n+3HoPnFgD5ioq2IbbHhZ/9/wfOLADahma5jozVo68TfwfOmg+dJAfIdAS+ODl7QE2IgE9yaguvi"
    "H6Mgcki/mnL45QaFU+irDdZsYzj7wG8hlPmBUQUJZLPbV2lmbRVka1+NHru+6gr5Lk6D1PDE4L6N"
    "BXcV9aMge3HKACcn30rLZrBVBxyqgagjPPETdLXn28A6fAOlQAlAX6fHW0m4LGmfFI6gFNo/nZao"
    "8iHtmLk+2YRK8rJwoHyDAJ2WqQEAwxgGBZAzz8D5BgBfp8eIoA2wYOgGfePA+SjwPkoMA+G1iqDf"
    "93f39vizUAgXmkFXTUGqrZ5RqGlPJvPPe/b8pxPC98E71TcY26bv8TmS/WFW7nvJ7xXk5URvvvEv"
    "7xDENbnQa/hKjT+EI7oypHStCYvuSpesfbECK5PmsH6TzVS5voABvWluRFnjKUljZuc4hCLsCbvG"
    "+lUvfZ5kShYlUDkwF+FA6Xv1X1EfESKfS7GSHBQjDoEWY0gkqwOM8tuXSvdyA7sinbyXcATN9W8r"
    "0zq6dVUJA/MV6+qfOl1D/9k="
)

PREMUL = base64.b64decode(
    "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"
)

# T.800, Table I.16: a channel of type 1 is opacity, of type 2 premultiplied
# opacity; association 0 applies it to the whole image.
OPACITY = 1
PREMULTIPLIED_OPACITY = 2


def cdef(alpha_type):
    """A Channel Definition box: three colour channels, then the opacity."""
    entries = [(0, 0, 1), (1, 0, 2), (2, 0, 3), (3, alpha_type, 0)]
    payload = struct.pack(">H", len(entries))
    for entry in entries:
        payload += struct.pack(">HHH", *entry)
    return _jp2._box(b"cdef", payload)


def container(codestream, alpha_type):
    """A 32 x 32 RGBA JP2 file in enumerated sRGB."""
    header = _jp2._box(b"ihdr", struct.pack(">IIHBBBB", SIZE, SIZE, 4, 7, 7, 0, 0))
    header += _jp2.colr(1, 0, _jp2.ENUM_SRGB)
    header += cdef(alpha_type)
    signature = struct.pack(">I", 12) + b"jP  " + b"\x0d\x0a\x87\x0a"
    file_type = _jp2._box(b"ftyp", b"jp2 " + struct.pack(">I", 0) + b"jp2 ")
    return signature + file_type + _jp2._box(b"jp2h", header) + _jp2._box(b"jp2c", codestream)


pdf = pikepdf.new()


def image(data, **entries):
    stream = Stream(pdf, data)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = SIZE
    stream.Height = SIZE
    stream.Filter = Name.JPXDecode
    for key, value in entries.items():
        stream[Name("/" + key)] = value
    return pdf.make_indirect(stream)


xobjects = Dictionary(
    ImStraight=image(container(STRAIGHT, OPACITY), SMaskInData=1),
    ImPremul=image(container(PREMUL, PREMULTIPLIED_OPACITY), SMaskInData=2, ColorSpace=Name.DeviceRGB),
)
bands = b"".join(
    b"%s rg 0 %d 200 10 re f\n" % (b"1 1 0" if i % 2 == 0 else b"0 0 1", i * 10) for i in range(11)
)
page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 200, 110]),
    Resources=Dictionary(XObject=xobjects),
    Contents=Stream(pdf, bands + b"q 90 0 0 90 5 10 cm /ImStraight Do Q\nq 90 0 0 90 105 10 cm /ImPremul Do Q\n"),
))
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "smaskindata_2b.pdf", min_version="1.7")
