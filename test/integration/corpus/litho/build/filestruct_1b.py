#!/usr/bin/env python3
"""Build filestruct_1b.pdf: file-level syntax and form rules under PDF/A-1b.

The coverage scan showed ISO 19005-1 6.1.2, 6.1.3, 6.1.6 and 6.9 unreached
between them. All four judge the file rather than any page: the header comment,
the trailer identifier, the lexical form of hexadecimal strings, and the
interactive form dictionary.

Six defects sit in one file, on four independent axes, which is the masking
probe: a check that reports the first syntax problem and stops reports one rule
where four are due, and a check that condemns every hexadecimal string once it
sees a malformed one fires on the conforming control beside them.

| Construct                                          | Rule       |
| -------------------------------------------------- | ---------- |
| header comment bytes below 128                     | `6.1.2/2`  |
| no `/ID` in the trailer                            | `6.1.3/1`  |
| hexadecimal string with an odd digit count         | `6.1.6/1`  |
| hexadecimal string containing a non-hex character  | `6.1.6/2`  |
| `/NeedAppearances true` on the AcroForm            | `6.9/1`    |
| a form field with no appearance dictionary         | `6.9/2`    |

The two 6.1.6 defects are deliberately kept apart. veraPDF tests them
separately, `hexCount % 2 == 0` against `containsOnlyHex == true`, so a single
string breaking both would report one rule and leave the other unproven: the
odd-count string uses only hex digits, and the non-hex string has an even count.

Three of the six cannot be written by pikepdf, which emits a conforming header
comment, always writes an `/ID`, and normalises every string it serialises. They
are patched into the saved bytes instead, each replacement the same length as
what it replaces, so every cross-reference offset stays valid. See
`limits_2b.py` and `xref_1b.py` for the same technique.

See ../README.md for the authoritative expected firing set.
"""

import re

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def appearance():
    """A minimal appearance stream, the value a widget's /N takes."""
    ap = Stream(pdf, b"0 0 0 rg 0 0 60 20 re f\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 60, 20])
    return pdf.make_indirect(ap)


# Placeholders patched into hexadecimal strings after saving. Each is exactly
# the length its replacement needs, counting the enclosing delimiters, so the
# patch never moves a byte. pikepdf writes String("ABCDEF") as "(ABCDEF)",
# eight bytes, which is room for "<AB CDE>" or "<ABCDZZ>".
HEX_ODD_PLACEHOLDER = "ABCDEF"
HEX_NONHEX_PLACEHOLDER = "GHIJKL"
# A well-formed hexadecimal string: even digit count, hex digits only. It is the
# control, and it is written as a placeholder too so all three take the same
# route into the file.
HEX_VALID_PLACEHOLDER = "MNOPQR"

widget_no_ap = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 600, 110, 620]),
        F=4,
        FT=Name.Tx,
        T=String("noap"),
        # 6.9 t2: no /AP at all.
    )
)

# The control for 6.9 t2: an otherwise identical widget that does carry one.
widget_with_ap = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 560, 110, 580]),
        F=4,
        FT=Name.Tx,
        T=String("hasap"),
        AP=Dictionary(N=appearance()),
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (file structure) Tj ET\n"),
        Annots=Array([widget_no_ap, widget_with_ap]),
    )
)
pdf.pages.append(pikepdf.Page(page))

# 6.9 t1: NeedAppearances present and true.
pdf.Root.AcroForm = Dictionary(
    Fields=Array([widget_no_ap, widget_with_ap]),
    NeedAppearances=True,
)

# The hexadecimal strings ride in the document information dictionary, which is
# lexed like any other object. Their values carry no meaning for these tests.
pdf.docinfo = pdf.make_indirect(
    Dictionary(
        Producer=String(HEX_ODD_PLACEHOLDER),
        Creator=String(HEX_NONHEX_PLACEHOLDER),
        Title=String(HEX_VALID_PLACEHOLDER),
    )
)

# Deliberately no /Metadata and no /OutputIntents, both independent of the
# clauses above.
save(pdf, "filestruct_1b.pdf", min_version="1.4")

path = out_path("filestruct_1b.pdf")
with open(path, "rb") as fh:
    data = fh.read()


def patch(data, old, new, why):
    """Replace old with new exactly once, refusing any length change."""
    if len(old) != len(new):
        raise SystemExit(f"{why}: patch changes length, offsets would move")
    if data.count(old) != 1:
        raise SystemExit(f"{why}: expected exactly one {old!r}, found {data.count(old)}")
    return data.replace(old, new)


# 6.1.2 t2: the four bytes after the second % must each exceed 127. pikepdf
# writes 0xBF 0xF7 0xA2 0xFE; four ASCII letters occupy the same four bytes
# and every one of them is below 128.
header = re.search(rb"%PDF-\d\.\d\n%(.{4})\n", data, re.S)
if header is None:
    raise SystemExit("header comment not found")
data = patch(data, b"%" + header.group(1) + b"\n", b"%LITH\n", "6.1.2 t2")

# 6.1.3 t1: the trailer must carry no /ID. Blanking the entry keeps the trailer
# the same length, and startxref points at the cross-reference table that
# precedes it, so nothing moves.
trailer_id = re.search(rb"/ID \[<[0-9a-fA-F]+><[0-9a-fA-F]+>\]", data)
if trailer_id is None:
    raise SystemExit("trailer /ID not found")
data = patch(data, trailer_id.group(0), b" " * len(trailer_id.group(0)), "6.1.3 t1")

# 6.1.6 t1: an odd number of non-white-space characters. The space inside is
# white space, so this string carries five hexadecimal digits.
data = patch(data, b"(" + HEX_ODD_PLACEHOLDER.encode() + b")", b"<AB CDE>", "6.1.6 t1")
# 6.1.6 t2: an even count, but Z is not a hexadecimal digit.
data = patch(
    data, b"(" + HEX_NONHEX_PLACEHOLDER.encode() + b")", b"<ABCDZZ>", "6.1.6 t2"
)
# The control: even count, hexadecimal digits only.
data = patch(
    data, b"(" + HEX_VALID_PLACEHOLDER.encode() + b")", b"<ABCDEF>", "6.1.6 control"
)

with open(path, "wb") as fh:
    fh.write(data)
print("patched", path, len(data), "bytes")
