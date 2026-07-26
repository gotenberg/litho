#!/usr/bin/env python3
"""Build fontdict_2b.pdf: font dictionary keys and page box limits.

A coverage scan showed the corpus never reached ISO 19005-2, 6.2.11.2 (the
required keys of a font dictionary) or 6.1.13 (implementation limits). Both
are authorable without a font program, so they cost nothing to plant.

The interesting part is the shape rather than the count. Four fonts are drawn
on the same page and each carries a *different* defect, so a check that
reports the first offending font and stops reports one rule where four are
due. A fifth, fully conforming font is drawn alongside them, so
over-suppression is visible too.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    pikepdf,
    save,
)

pdf = pikepdf.new()


def descriptor(name):
    return Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=32,
        FontBBox=Array([-166, -225, 1000, 931]),
        ItalicAngle=0,
        Ascent=718,
        Descent=-207,
        CapHeight=718,
        StemV=88,
    )


def simple_font(name, first=None, last=None, widths=None, base=True):
    d = Dictionary(Type=Name.Font, Subtype=Name.TrueType, Encoding=Name.WinAnsiEncoding)
    if base:
        d.BaseFont = Name("/" + name)
        d.FontDescriptor = pdf.make_indirect(descriptor(name))
    if first is not None:
        d.FirstChar = first
    if last is not None:
        d.LastChar = last
    if widths is not None:
        d.Widths = Array(widths)
    return pdf.make_indirect(d)


# F1 omits /FirstChar (ISO 19005-2, 6.2.11.2 t4). The font is not one of the
# standard 14, so the exemption does not apply.
f1 = simple_font("LithoAlpha", last=66, widths=[722, 722])

# F2 omits /LastChar (t5).
f2 = simple_font("LithoBeta", first=65, widths=[722, 722])

# F3 declares three codes but supplies two widths (t6).
f3 = simple_font("LithoGamma", first=65, last=67, widths=[722, 722])

# F4 omits /BaseFont entirely (t3).
f4 = simple_font("LithoDelta", first=65, last=66, widths=[722, 722], base=False)

# F5 conforms in every respect. A firing here would be a false positive, and
# its silence shows the check did not latch on to the first offender.
f5 = simple_font("LithoEpsilon", first=65, last=66, widths=[722, 722])

content = b"""BT /F1 12 Tf 50 700 Td (AB) Tj ET
BT /F2 12 Tf 50 670 Td (AB) Tj ET
BT /F3 12 Tf 50 640 Td (AB) Tj ET
BT /F4 12 Tf 50 610 Td (AB) Tj ET
BT /F5 12 Tf 50 580 Td (AB) Tj ET
"""

page1 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=f1, F2=f2, F3=f3, F4=f4, F5=f5)),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page1)))

# Page 2 is two units square. ISO 19005-2, 6.1.13 t11 requires every page box
# edge to span at least 3 and at most 14400 units, so both edges fail.
page2 = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 2, 2]),
    Resources=Dictionary(),
    Contents=Stream(pdf, b"0 0 1 1 re f\n"),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page2)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "fontdict_2b.pdf", min_version="1.7")
