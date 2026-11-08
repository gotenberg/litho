#!/usr/bin/env python3
"""Build fontkeys_1b.pdf: font dictionary required keys under PDF/A-1b.

`fontdict_2b.pdf` covers the part-2 spelling of this requirement, ISO 19005-2,
6.2.11.2, which has four tests. Part 1 states it as ISO 19005-1, 6.3.2 and
splits it into seven, so two of them are reachable from no other fixture: the
`/Type` key of the font dictionary itself and the subtype of an embedded font
file.

Five sibling fonts carry one defect each and are all drawn on the page, which
is the masking probe: a check that reports the first offending font and stops
reports one rule where five are due.

Tests 2 and 3 are deliberately absent, and neither can be reached from any
fixture. veraPDF builds a font object only for the five subtypes test 2
permits, so a font naming any other reaches no test at all. And a font with no
`/BaseFont`, which is what test 3 judges, makes veraPDF throw at PDF/A-1b: the
identical file validates at PDF/A-2b and reports the part-2 spelling of the
same requirement. litho reports both. See ../FIXTURES.md.

The clause splits by object, which decides which font can carry which defect.
Tests 1 and 7 are scoped to `PDFont` and apply to every font; tests 4, 5
and 6 are scoped to `PDSimpleFont` and exempt a standard font, so the fonts
carrying them name a face that is not one of the standard 14. That exemption is
itself planted as a control: a Helvetica with no `/FirstChar`, `/LastChar` or
`/Widths` conforms, and a check that ignores `isStandard` fires on it.

A stream carrying an `/F` key sits beside the fonts. ISO 19005-1, 6.1.7
forbids it, it belongs to no font clause, and it gives the file a defect from a
different part of the specification entirely.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()


def descriptor(name, embedded=None):
    """A font descriptor, optionally carrying an embedded font file."""
    d = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=32,
        ItalicAngle=0,
        Ascent=718,
        Descent=-207,
        CapHeight=718,
        StemV=88,
        FontBBox=Array([-166, -225, 1000, 931]),
    )
    if embedded is not None:
        d.FontFile3 = embedded
    return pdf.make_indirect(d)


def font(name, *, type_key=True, subtype="Type1", basefont=True,
         first_char=65, last_char=66, widths=(600, 600), embedded=None):
    """One simple font dictionary, conforming unless an argument says otherwise."""
    d = Dictionary()
    if type_key:
        d.Type = Name.Font
    if subtype is not None:
        d.Subtype = Name("/" + subtype)
    if basefont:
        d.BaseFont = Name("/" + name)
    if first_char is not None:
        d.FirstChar = first_char
    if last_char is not None:
        d.LastChar = last_char
    if widths is not None:
        d.Widths = Array(list(widths))
    d.FontDescriptor = descriptor(name, embedded)
    return pdf.make_indirect(d)


# A font file whose /Subtype is neither Type1C nor CIDFontType0C. The test reads
# the key rather than the bytes, so the stream carries a placeholder. The
# subtype is /Type1 rather than /OpenType because veraPDF parses an OpenType
# font file eagerly and throws on a placeholder, which aborts the whole run.
bad_fontfile = Stream(pdf, b"\x01\x00\x04\x01placeholder")
bad_fontfile.Subtype = Name.Type1
bad_fontfile = pdf.make_indirect(bad_fontfile)

fonts = {
    # F1 omits /Type, which the clause requires on every font dictionary.
    "F1": font("LithoNoType", type_key=False),
    # F4, F5 and F6 name a face outside the standard 14, so the exemption that
    # protects a standard font does not apply to them.
    "F4": font("LithoNoFirst", first_char=None),
    "F5": font("LithoNoLast", last_char=None),
    # Two codes are declared and three widths supplied.
    "F6": font("LithoBadWidths", widths=(600, 600, 600)),
    # F7 embeds a font file whose subtype the clause does not permit.
    "F7": font("LithoBadFile", embedded=bad_fontfile),
    # F8 is a standard font with none of /FirstChar, /LastChar or /Widths,
    # which tests 4, 5 and 6 exempt. A firing here is over-suppression of the
    # isStandard escape.
    "F8": font("Helvetica", first_char=None, last_char=None, widths=None),
    # F9 conforms in every respect.
    "F9": font("LithoConforming"),
}

content = b"".join(
    b"BT /%s 12 Tf 50 %d Td (AB) Tj ET\n" % (key.encode(), 720 - i * 20)
    for i, key in enumerate(sorted(fonts))
)

# A stream dictionary carrying /F, which ISO 19005-1, 6.1.7 forbids outright.
# It is an XObject rather than a font resource, so it belongs to no font clause.
stray = Stream(pdf, b"0 0 0 rg 0 0 10 10 re f\n")
stray.Type = Name.XObject
stray.Subtype = Name.Form
stray.BBox = Array([0, 0, 10, 10])
stray.F = String("external.bin")

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(**fonts),
            XObject=Dictionary(Fm0=pdf.make_indirect(stray)),
        ),
        Contents=Stream(pdf, content + b"q 1 0 0 1 400 700 cm /Fm0 Do Q\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "fontkeys_1b.pdf", min_version="1.4")
