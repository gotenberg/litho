#!/usr/bin/env python3
"""Build charset_1b.pdf: what a subset font's /CharSet and /CIDSet must list.

A coverage scan showed twelve cells no fixture reached, all of them the same
two clauses read at four different strictnesses:

| Rule                          | Reads                                           |
| ----------------------------- | ----------------------------------------------- |
| ISO 19005-1, 6.3.5 t2/t3      | a Type 1 or CID subset must carry the entry     |
| ISO 19005-2/3, 6.2.11.4.2 t1  | a `/CharSet` present must list every glyph      |
| ISO 19005-2/3, 6.2.11.4.2 t2  | a `/CIDSet` present must identify every CID     |
| ISO 14289-1, 7.21.4.2 t1/t2   | the PDF/UA-1 spelling of the same two           |

Part 1 requires the entry to exist; parts 2 and 3 permit it to be absent and
judge it only once present. One font cannot carry both defects, so the file
carries eight, four per family:

| Font       | `/BaseFont`       | Entry            | Answers                    |
| ---------- | ----------------- | ---------------- | -------------------------- |
| `T1None`   | `ABCDEF+LithoCFF` | no `/CharSet`    | 6.3.5 t2 at part 1 only    |
| `T1Short`  | `BCDEFG+LithoCFF` | `/CharSet (/A)`  | the completeness tests     |
| `T1Full`   | `CDEFGH+LithoCFF` | `/CharSet (/A/B)`| nothing                    |
| `T1Whole`  | `LithoCFF`        | no `/CharSet`    | nothing: not a subset      |
| `CIDNone`  | `DEFGHI+LithoCID` | no `/CIDSet`     | 6.3.5 t3 at part 1 only    |
| `CIDShort` | `EFGHIJ+LithoCID` | `/CIDSet` less 2 | the completeness tests     |
| `CIDFull`  | `FGHIJK+LithoCID` | full `/CIDSet`   | nothing                    |
| `CIDWhole` | `LithoCID`        | no `/CIDSet`     | nothing: not a subset      |

Every font is painted, and each shows both glyphs the program defines. That is
the masking probe twice over: the two families answer independently at every
profile, so a checker that reports the first offending font and stops reports
one rule where two are due. The two `Whole` fonts are the subset-prefix
exemption planted as a control, and the two `Full` fonts are the conforming
case of the very entry the others break.

The subset prefixes differ per font on purpose. veraPDF keys a font object on
its name, so two fonts sharing `ABCDEF+LithoCFF` would collapse into one and
the control would never be judged separately from the defect.

`T1Short` renders `B`, which is what makes it answer at part 1 as well: ISO
19005-1, 6.3.5 t2 is one-directional and scoped to the glyphs the content
paints, while ISO 19005-2, 6.2.11.4.2 t1 compares against the whole program.
Both readings condemn a `/CharSet` of `(/A)` on a font that paints `B`.

The programs come from `_cff.py` and `_truetype.py`, which emit the bytes
directly: a validator has to parse a real program to know which glyphs it
defines, and a placeholder stream leaves every completeness test silent.

Neither `/Metadata` nor an output intent is present, so the file also answers
the metadata and colour clauses. They belong to different parts of the
specification entirely and give the font verdicts something to hide behind.

See ../README.md for the authoritative expected firing set.
"""

import _cff
import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

CFF_PROGRAM = _cff.build()
TT_PROGRAM = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])

NONSYMBOLIC = 32
SYMBOLIC = 4

# A /CIDSet is a bitmap indexed by CID, most significant bit first, so CIDs 1
# and 2 are bits 6 and 5 of the first byte. CID 0 is .notdef and exempt.
CIDSET_FULL = bytes([0b01100000])
CIDSET_SHORT = bytes([0b01000000])


def cff_descriptor(name, charset):
    """A Type 1 descriptor carrying the bare CFF, with /CharSet when asked."""
    program = Stream(pdf, CFF_PROGRAM)
    program.Subtype = Name("/Type1C")
    d = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=NONSYMBOLIC,
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontBBox=Array([0, -200, 550, 700]),
        FontFile3=pdf.make_indirect(program),
    )
    if charset is not None:
        d.CharSet = String(charset)
    return pdf.make_indirect(d)


def type1_font(name, charset):
    """One simple font over the CFF program, conforming apart from /CharSet."""
    return pdf.make_indirect(Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/" + name),
        FirstChar=65,
        LastChar=66,
        Widths=Array([600, 600]),
        Encoding=Name.WinAnsiEncoding,
        FontDescriptor=cff_descriptor(name, charset)))


def tt_descriptor(name, cidset):
    """A CIDFont descriptor carrying the TrueType program, /CIDSet when asked."""
    program = Stream(pdf, TT_PROGRAM)
    program.Length1 = len(TT_PROGRAM)
    d = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=SYMBOLIC,
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontBBox=Array([0, -200, 550, 700]),
        FontFile2=pdf.make_indirect(program),
    )
    if cidset is not None:
        d.CIDSet = pdf.make_indirect(Stream(pdf, cidset))
    return pdf.make_indirect(d)


def cid_font(name, cidset):
    """A Type 0 font over a CIDFontType2 descendant, Identity-H encoded."""
    descendant = pdf.make_indirect(Dictionary(
        Type=Name.Font,
        Subtype=Name.CIDFontType2,
        BaseFont=Name("/" + name),
        CIDSystemInfo=Dictionary(
            Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0),
        FontDescriptor=tt_descriptor(name, cidset),
        DW=600,
        CIDToGIDMap=Name.Identity))
    return pdf.make_indirect(Dictionary(
        Type=Name.Font,
        Subtype=Name.Type0,
        BaseFont=Name("/" + name),
        Encoding=Name("/Identity-H"),
        DescendantFonts=Array([descendant])))


fonts = Dictionary(
    T1None=type1_font("ABCDEF+LithoCFF", None),
    T1Short=type1_font("BCDEFG+LithoCFF", "/A"),
    T1Full=type1_font("CDEFGH+LithoCFF", "/A/B"),
    T1Whole=type1_font("LithoCFF", None),
    CIDNone=cid_font("DEFGHI+LithoCID", None),
    CIDShort=cid_font("EFGHIJ+LithoCID", CIDSET_SHORT),
    CIDFull=cid_font("FGHIJK+LithoCID", CIDSET_FULL),
    CIDWhole=cid_font("LithoCID", None),
)

# Every font shows both glyphs its program defines. A font declared in
# /Resources but never selected by a Tf is reached by neither engine.
lines = []
y = 720
for key in ("T1None", "T1Short", "T1Full", "T1Whole"):
    lines.append(b"BT /%s 12 Tf 50 %d Td (AB) Tj ET" % (key.encode(), y))
    y -= 30
for key in ("CIDNone", "CIDShort", "CIDFull", "CIDWhole"):
    lines.append(b"BT /%s 12 Tf 50 %d Td <00010002> Tj ET" % (key.encode(), y))
    y -= 30
content = b"\n".join(lines) + b"\n"

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=fonts),
    Contents=Stream(pdf, content)))
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
save(pdf, "charset_1b.pdf")
