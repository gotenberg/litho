#!/usr/bin/env python3
"""Build a4syntax_4.pdf: file and document level rules under PDF/A-4.

The coverage scan showed twenty-one ISO 19005-4 rules unreached outside the
JPEG2000 family. Eleven of them judge the file or the catalog rather than any
page, and they are independent of one another, which is what makes them worth
putting in one file: a check that abandons a document after the first syntax
complaint reports one rule where eight axes are due.

| Construct                                          | Rule        |
| -------------------------------------------------- | ----------- |
| a stream whose `/Length` is not its byte count     | `6.1.6.1/1` |
| a name object that is not valid UTF-8              | `6.1.7/1`   |
| an indirect object header with non-conforming spacing | `6.1.8/1` |
| an `LZWDecode` filter                              | `6.1.9/1`   |
| a `/Perms` entry other than `UR3` or `DocMDP`      | `6.1.11/1`  |
| a catalog `/Version` that is not 2.x               | `6.1.12/1`  |
| a widget annotation carrying `/A`                  | `6.4.1/1`   |
| a named action other than the four permitted       | `6.6.1/2`   |
| `pdfaid:rev` bound to a prefix other than `pdfaid` | `6.7.3/6`   |
| two optional content groups sharing a `/Name`      | `6.10/2`    |
| `/AlternatePresentations` in the name dictionary   | `6.11/1`    |

Controls sit beside the defects wherever the test turns on a value rather than
on presence: a widget with no `/A`, a `/NextPage` named action which is one of
the four the clause permits, a third optional content group with a distinct
name, and the page content stream, whose `/Length` and spacing are both correct.

Four of the eleven are beyond pikepdf, which computes `/Length` from the data,
normalises object headers, and refuses to emit a name that is not valid UTF-8.
They are patched into the saved bytes, each replacement exactly the length of
what it replaces, so no cross-reference offset moves. The builder refuses any
patch that would change a length rather than silently corrupting the file. See
`filestruct_1b.py` and `xref_1b.py` for the same technique.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import re

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    lzw_encode,
    out_path,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# 6.1.6.1 t1 rides on its own Form XObject, marked so the byte patch below can
# find exactly one occurrence. The page content stream beside it stays correct
# and is the control.
BAD_LENGTH_MARK = b"% litho bad length marker\n"

bad_length = Stream(pdf, BAD_LENGTH_MARK + b"0.5 0.5 0.5 rg 0 0 20 20 re f\n")
bad_length.Type = Name.XObject
bad_length.Subtype = Name.Form
bad_length.BBox = Array([0, 0, 20, 20])
bad_length.Resources = Dictionary()
bad_length = pdf.make_indirect(bad_length)

# An LZWDecode stream, forbidden as a stream filter. It sits beside the inline
# image below because the two are judged by different veraPDF objects, a stream
# filter against an inline image filter, and only the inline one reaches 6.1.9.
# The data is genuinely LZW-compressed so a validator has to reject the filter
# by name rather than by failing to decode it.
lzw_payload = b"0 0 1 rg 0 0 20 20 re f\n"
lzw_form = Stream(pdf, lzw_encode(lzw_payload))
lzw_form.Type = Name.XObject
lzw_form.Subtype = Name.Form
lzw_form.BBox = Array([0, 0, 20, 20])
lzw_form.Resources = Dictionary()
lzw_form.Filter = Name.LZWDecode
lzw_form = pdf.make_indirect(lzw_form)

# 6.1.7 t1: a name object whose bytes are not valid UTF-8. veraPDF checks names
# in a few modelled positions only, and a Separation colorant name is one of
# them; an unknown catalog key, a resource key, a /Differences glyph name and an
# annotation /Name were all measured silent. pikepdf will not emit an invalid
# name, so a placeholder of the exact width goes in and the patch below rewrites
# it: seven bytes either way, /AAAAAA against /#C0#AF, where C0 AF is an
# overlong encoding and so not valid UTF-8.
UTF8_PLACEHOLDER = "AAAAAA"

tint = Stream(pdf, b"{ }")
tint.FunctionType = 4
tint.Domain = Array([0, 1])
tint.Range = Array([0, 1])
tint = pdf.make_indirect(tint)

bad_colorant = pdf.make_indirect(
    Array([Name.Separation, Name("/" + UTF8_PLACEHOLDER), Name.DeviceGray, tint])
)
# The control: an otherwise identical Separation whose colorant name is ASCII.
good_colorant = pdf.make_indirect(
    Array([Name.Separation, Name("/Spot"), Name.DeviceGray, tint])
)


def appearance():
    ap = Stream(pdf, b"0 0 0 rg 0 0 60 20 re f\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 60, 20])
    return pdf.make_indirect(ap)


# 6.4.1 t1: a widget carrying /A, beside one that carries none.
widget_with_action = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 600, 110, 620]),
        F=4,
        FT=Name.Btn,
        T=String("acts"),
        AP=Dictionary(N=appearance()),
        A=Dictionary(S=Name.Named, N=Name.NextPage),
    )
)
widget_without_action = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 560, 110, 580]),
        F=4,
        FT=Name.Btn,
        T=String("inert"),
        AP=Dictionary(N=appearance()),
    )
)

# 6.10 t2 judges optional content CONFIGURATIONS, not the groups themselves:
# duplicate group names were measured silent. The groups here carry distinct
# names on purpose, so the file proves the rule is about configurations.
ocg_a = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer one")))
ocg_b = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("layer two")))

# 6.1.9 t1 judges an INLINE image filter, which is a different veraPDF object
# from the stream filter above. /LZW is the inline abbreviation of LZWDecode;
# the second inline image uses /AHx, one the clause permits, and is the control.
INLINE_IMAGES = (
    b"q 20 0 0 20 60 360 cm\n"
    b"BI /W 2 /H 2 /CS /G /BPC 8 /F /LZW ID \x80\x0b\x60\x50\x22\x0c\x0c\x85 EI Q\n"
    b"q 20 0 0 20 100 360 cm\n"
    b"BI /W 2 /H 2 /CS /G /BPC 8 /F /AHx ID 00FF00FF> EI Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            XObject=Dictionary(FmL=bad_length, FmZ=lzw_form),
            ColorSpace=Dictionary(CsBad=bad_colorant, CsOk=good_colorant),
            Properties=Dictionary(OcA=ocg_a, OcB=ocg_b),
        ),
        Contents=Stream(
            pdf,
            b"BT /F1 12 Tf 50 700 Td (file and document rules) Tj ET\n"
            b"q 1 0 0 1 60 400 cm /FmL Do Q\n"
            b"q 1 0 0 1 120 400 cm /FmZ Do Q\n"
            b"q /CsBad cs 0.5 scn 60 320 20 20 re f Q\n"
            b"q /CsOk cs 0.5 scn 100 320 20 20 re f Q\n" + INLINE_IMAGES,
        ),
        Annots=Array([widget_with_action, widget_without_action]),
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AcroForm = Dictionary(
    Fields=Array([widget_with_action, widget_without_action])
)
# The default configuration and one alternate share a /Name, which is the
# defect; the second alternate carries a distinct name and is the control.
pdf.Root.OCProperties = Dictionary(
    OCGs=Array([ocg_a, ocg_b]),
    D=Dictionary(
        Name=String("shared"), Order=Array([ocg_a, ocg_b]), ON=Array([ocg_a, ocg_b])
    ),
    Configs=Array(
        [
            Dictionary(
                Name=String("shared"),
                Order=Array([ocg_a, ocg_b]),
                ON=Array([ocg_a, ocg_b]),
            ),
            Dictionary(
                Name=String("distinct"),
                Order=Array([ocg_a, ocg_b]),
                ON=Array([ocg_a, ocg_b]),
            ),
        ]
    ),
)

# 6.6.1 t2: a named action the clause does not permit, beside a permitted one.
outline_bad = pdf.make_indirect(
    Dictionary(
        Title=String("forbidden named action"),
        A=Dictionary(S=Name.Named, N=Name("/GoForward")),
    )
)
outline_ok = pdf.make_indirect(
    Dictionary(
        Title=String("permitted named action"),
        A=Dictionary(S=Name.Named, N=Name.NextPage),
    )
)
outline_bad.Next = outline_ok
outline_ok.Prev = outline_bad
outlines = pdf.make_indirect(
    Dictionary(Type=Name.Outlines, First=outline_bad, Last=outline_ok, Count=2)
)
outline_bad.Parent = outlines
outline_ok.Parent = outlines
pdf.Root.Outlines = outlines

# 6.1.11 t1: /Perms may name only UR3 and DocMDP.
pdf.Root.Perms = Dictionary(Foo=Dictionary(Type=Name.SigRef))

# 6.1.12 t1: the catalog /Version must be a PDF 2.x version.
pdf.Root.Version = Name("/1.7")

# 6.11 t1: alternate presentations are forbidden outright.
pdf.Root.Names = Dictionary(AlternatePresentations=Dictionary(Names=Array()))

# 6.7.3 t6: pdfaid:rev must be bound to the pdfaid prefix. Declaring a second
# prefix on the same namespace and putting rev under it leaves the claim
# findable but the prefix wrong. pdfaid:part stays on the expected prefix so
# the file is still identified as PDF/A-4.
attach_xmp(
    pdf,
    xmp_packet(
        part=4,
        extra='    <pdfaid2:rev>2020</pdfaid2:rev>',
    ).replace(
        b'xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"',
        b'xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"\n'
        b'    xmlns:pdfaid2="http://www.aiim.org/pdfa/ns/id/"',
    ),
)

save(pdf, "a4syntax_4.pdf", min_version="2.0")

path = out_path("a4syntax_4.pdf")
with open(path, "rb") as fh:
    data = fh.read()


def patch(data, old, new, why):
    """Replace old with new exactly once, refusing any length change."""
    if len(old) != len(new):
        raise SystemExit(f"{why}: patch changes length, offsets would move")
    if data.count(old) != 1:
        raise SystemExit(f"{why}: expected exactly one match, found {data.count(old)}")
    return data.replace(old, new)


# 6.1.6.1 t1: /Length must equal the byte count that follows. The declared value
# sits immediately before the stream keyword introducing the marked payload.
# Scanning forward avoids the empty /Resources dictionary nested in the same
# object, which a backwards search for "<<" would find instead.
payload = data.index(BAD_LENGTH_MARK)
match = None
for m in re.finditer(rb"/Length (\d+)([^>]*)>>\s*stream\r?\n", data[:payload]):
    match = m
if match is None:
    raise SystemExit("6.1.6.1 t1: /Length before the marked stream not found")
digits = match.group(1)
wrong = str(int(digits) + 1).encode()
if len(wrong) != len(digits):
    raise SystemExit("6.1.6.1 t1: differing length would change the digit count")
data = data[: match.start(1)] + wrong + data[match.end(1) :]

# 6.1.7 t1: C0 AF is an overlong encoding, which UTF-8 forbids. The colorant
# name appears once, inside the Separation array; the /CsBad key that names the
# colour space in /Resources is a different name and is left alone.
data = patch(data, b"/" + UTF8_PLACEHOLDER.encode(), b"/#C0#AF", "6.1.7 t1")

# 6.1.8 t1: the object header takes exactly one space between each token.
# Moving one of the two spaces keeps the header the same width.
header = re.search(rb"\n(\d+) 0 obj\n", data)
if header is None:
    raise SystemExit("6.1.8 t1: no object header found")
num = header.group(1)
data = patch(
    data,
    b"\n" + num + b" 0 obj\n",
    b"\n" + num + b"  0 obj",
    "6.1.8 t1",
)

with open(path, "wb") as fh:
    fh.write(data)
print("patched", path, len(data), "bytes")
