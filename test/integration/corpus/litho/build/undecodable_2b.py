#!/usr/bin/env python3
"""Build undecodable_2b.pdf: content streams veraPDF reads no operator from.

veraPDF 1.30 decodes a stream through five filters alone, FlateDecode,
ASCIIHexDecode, ASCII85Decode, LZWDecode and RunLengthDecode, and reads no
operator from a content stream whose chain ends with any other name: the
abbreviations of inline images, the image codecs, BrotliDecode, an unknown
name. A name it does not decode earlier in the chain passes its input on
unchanged. Nor does it read a /Contents that is neither a stream nor an array.
litho read the operators of streams in filters it decodes itself, judged every
font of a page whose content it could not read, and read a Type 3 glyph's width
from a procedure veraPDF cannot read either. The corpus files that reach the
abbreviations, BrotliDecode, an image codec or a /Contents that is no stream
carry no compliance golden, and no corpus file writes a filter veraPDF does not
decode before the last of a chain, or parameters that do not fit their chain.

Every page paints a probe wherever a reader takes its operators, each with a
font of its own: it shows text in an unembedded font, fills in DeviceRGB with
no OutputIntent, and runs an undefined operator. Page 1 is the control.

| Page | Where the probe sits                                     | Read |
| ---- | -------------------------------------------------------- | ---- |
| 1    | the page, no filter                                      | yes  |
| 2    | the page, `[/Foo /FlateDecode]` over Flate data          | yes  |
| 3    | the page, `[/FlateDecode /Foo]` over Flate data          | no   |
| 4    | the page, `/Fl` over Flate data                          | no   |
| 5    | the page, `/DCTDecode` over the probe itself             | no   |
| 6    | the page, `/BrotliDecode` over Brotli data               | no   |
| 7    | none: `/Contents` is a dictionary                        | no   |
| 8    | a form XObject, `/Fl` over Flate data                    | no   |
| 9    | a tiling pattern cell, `/AHx` over hex digits            | no   |
| 10   | an annotation appearance, `/A85` over base-85            | no   |
| 12   | the page, `/FlateDecode` with two parameter dictionaries | no   |
| 13   | the page, `/FlateDecode` with the parameters `[5]`       | no   |

Page 2 gives `/Foo` a PNG predictor and `/FlateDecode` null, so the chain is
read without `/Foo` only if the parameters leave with it. veraPDF fails to
decode a stream whose parameters count other than its filters, or hold an
entry that is neither a dictionary nor null.

Page 11 shows the one glyph of a Type 3 font whose procedure is `/AHx` over
`500 0 d0`, under a `/Widths` of 500. veraPDF cannot read the procedure, takes
the missing width of 0, and fails the glyph width.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import base64
import zlib

from _common import Array, Dictionary, Name, Stream, pikepdf, save

PROBE = b"BT /F1 12 Tf 10 10 Td (A) Tj ET 1 0 0 rg 0 0 5 5 re f zzfoo\n"


def brotli_stored(data):
    """Encode data as a Brotli stream of one uncompressed meta-block.

    RFC 7932: a 16-bit window (9.1), a meta-block that is not last, whose
    length takes four nibbles and whose data is uncompressed (9.2), and an
    empty last meta-block.
    """
    out = bytearray()
    acc = 0
    nbits = 0

    def put(value, width):
        nonlocal acc, nbits
        acc |= value << nbits
        nbits += width
        while nbits >= 8:
            out.append(acc & 0xFF)
            acc >>= 8
            nbits -= 8

    def align():
        nonlocal acc, nbits
        if nbits:
            out.append(acc & 0xFF)
        acc = 0
        nbits = 0

    assert 0 < len(data) <= 1 << 16
    put(0, 1)  # WBITS = 16
    put(0, 1)  # ISLAST = 0
    put(0, 2)  # MNIBBLES = 4
    put(len(data) - 1, 16)  # MLEN - 1
    put(1, 1)  # ISUNCOMPRESSED
    align()
    out += data
    put(1, 1)  # ISLAST
    put(1, 1)  # ISLASTEMPTY
    align()
    return bytes(out)


pdf = pikepdf.new()


def probe_font():
    return pdf.make_indirect(
        Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name("/Helvetica"))
    )


def filtered(data, filters):
    stream = Stream(pdf, data)
    stream.Filter = filters
    return stream


def add_page(contents, resources, **extra):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 200, 200]),
            Resources=resources,
            Contents=contents,
            **extra,
        )
    )
    pdf.pages.append(pikepdf.Page(page))


def probe_page(contents):
    add_page(contents, Dictionary(Font=Dictionary(F1=probe_font())))


flate = zlib.compress(PROBE)

probe_page(Stream(pdf, PROBE))
foo_flate = filtered(flate, Array([Name("/Foo"), Name.FlateDecode]))
foo_flate.DecodeParms = Array([Dictionary(Predictor=12, Columns=4), None])
probe_page(foo_flate)
probe_page(filtered(flate, Array([Name.FlateDecode, Name("/Foo")])))
probe_page(filtered(flate, Name("/Fl")))
probe_page(filtered(PROBE, Name.DCTDecode))
probe_page(filtered(brotli_stored(PROBE), Name("/BrotliDecode")))
probe_page(pdf.make_indirect(Dictionary(NotAStream=True)))

form = filtered(flate, Name("/Fl"))
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 100, 100])
form.Resources = Dictionary(Font=Dictionary(F1=probe_font()))
add_page(Stream(pdf, b"/Fm0 Do\n"), Dictionary(XObject=Dictionary(Fm0=form)))

cell = filtered(PROBE.hex().encode() + b">", Name("/AHx"))
cell.Type = Name.Pattern
cell.PatternType = 1
cell.PaintType = 1
cell.TilingType = 1
cell.BBox = Array([0, 0, 10, 10])
cell.XStep = 10
cell.YStep = 10
cell.Resources = Dictionary(Font=Dictionary(F1=probe_font()))
add_page(
    Stream(pdf, b"/Pattern cs /P0 scn 0 0 100 100 re f\n"),
    Dictionary(Pattern=Dictionary(P0=cell)),
)

appearance = filtered(base64.a85encode(PROBE) + b"~>", Name("/A85"))
appearance.Type = Name.XObject
appearance.Subtype = Name.Form
appearance.BBox = Array([0, 0, 50, 50])
appearance.Resources = Dictionary(Font=Dictionary(F1=probe_font()))
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Square,
        Rect=Array([10, 10, 60, 60]),
        F=4,
        AP=Dictionary(N=appearance),
    )
)
add_page(Stream(pdf, b"0 0 m\n"), Dictionary(), Annots=Array([annot]))

glyph = filtered(b"500 0 d0 0 0 500 500 re f".hex().encode() + b">", Name("/AHx"))
type3 = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 500, 500]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=Dictionary(a=pdf.make_indirect(glyph)),
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array([97, Name("/a")])),
        FirstChar=97,
        LastChar=97,
        Widths=Array([500]),
        Resources=Dictionary(),
    )
)
add_page(
    Stream(pdf, b"BT /T3 12 Tf 10 10 Td (a) Tj ET\n"),
    Dictionary(Font=Dictionary(T3=type3)),
)

two_parms = filtered(flate, Name.FlateDecode)
two_parms.DecodeParms = Array([Dictionary(), Dictionary()])
probe_page(two_parms)
bad_parms = filtered(flate, Name.FlateDecode)
bad_parms.DecodeParms = Array([5])
probe_page(bad_parms)

# Deliberately no /Metadata.
save(pdf, "undecodable_2b.pdf")
