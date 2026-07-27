#!/usr/bin/env python3
"""Build ttencoding_2b.pdf: TrueType encoding keys in the font dictionary.

The coverage scan showed ISO 19005-2, 6.2.11.6 unreached. Its four tests split
by object, and that split decides what a fixture can reach: tests 2 and 3 scope
to PDTrueTypeFont and read only the font dictionary, while tests 1 and 4 scope
to TrueTypeFontProgram and need an embedded program with particular cmap
subtables. This fixture closes the first pair. No corpus fixture embeds a
TrueType program yet, so the second pair stays open.

Four sibling TrueType fonts, each carrying at most one defect, are all drawn on
the page. Two are defective and two conform, so a check that reports the first
offending font and stops reports one rule where two are due, and a check that
condemns every TrueType once it sees one wrongly reports the other two.

`MacExpertEncoding` would be the other route to test 2, and it is deliberately
absent: it defines no glyph at 'A' or 'B', so the codes resolve to U+0000 and
drag in 6.2.11.8 test 1 and 6.2.11.7.2 test 2, neither of which this fixture is
about. The no-encoding font reaches the same test without them.

The `/Differences` route to test 2 is deliberately absent. The test permits
differences whose glyph names are Unicode compliant, and veraPDF accepts every
difference on a font with no embedded program, including names that are plainly
not compliant, while litho reports them. Planting one would add a disagreeing
cell rather than coverage. See ../README.md.

Per part the clause is ISO 19005-1, 6.3.6, ISO 19005-2 and -3, 6.2.11.6, and
ISO 19005-4, 6.2.10.6.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

# Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
# one. Per ISO 32000-1, Table 123 the two are exclusive, and every test of this
# clause turns on which one is set.
SYMBOLIC = 4
NONSYMBOLIC = 32


def truetype(name, flags, encoding):
    """One simple TrueType font dictionary. encoding=None omits the key."""
    descriptor = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=flags,
        ItalicAngle=0,
        Ascent=718,
        Descent=-207,
        CapHeight=718,
        StemV=88,
        FontBBox=Array([-166, -225, 1000, 931]),
        # Deliberately no /FontFile2: the two tests this fixture reaches read
        # the dictionary, and an embedded program would additionally reach the
        # cmap tests this fixture cannot cover.
    )
    font = Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/" + name),
        FirstChar=65,
        LastChar=66,
        Widths=Array([722, 722]),
        FontDescriptor=pdf.make_indirect(descriptor),
    )
    if encoding is not None:
        font.Encoding = Name("/" + encoding)
    return pdf.make_indirect(font)


# T1: a symbolic font shall carry no /Encoding at all (6.2.11.6 t3).
t1 = truetype("LithoSymEnc", SYMBOLIC, "WinAnsiEncoding")
# T2: a non-symbolic font shall name MacRoman or WinAnsi; this one names
# nothing at all, so there is no encoding to satisfy the rule (t2).
t2 = truetype("LithoNonSymNone", NONSYMBOLIC, None)
# T3 and T4 conform, so a firing against either is over-suppression.
t3 = truetype("LithoSymOK", SYMBOLIC, None)
t4 = truetype("LithoNonSymOK", NONSYMBOLIC, "WinAnsiEncoding")

# An ExtGState carrying a transfer function, which ISO 19005-2, 6.2.5 forbids
# outright. It belongs to no font clause, so it shows a font verdict does not
# leak into unrelated checks, and gives the page a second independent defect.
transfer = Stream(pdf, b"{ 1 exch sub }")
transfer.FunctionType = 4
transfer.Domain = Array([0, 1])
transfer.Range = Array([0, 1])
gs = pdf.make_indirect(Dictionary(Type=Name.ExtGState, TR=pdf.make_indirect(transfer)))

content = (
    b"q /GS1 gs 0 0 0 rg 50 730 40 20 re f Q\n"
    b"BT /T1 12 Tf 50 700 Td (AB) Tj ET\n"
    b"BT /T2 12 Tf 50 680 Td (AB) Tj ET\n"
    b"BT /T3 12 Tf 50 660 Td (AB) Tj ET\n"
    b"BT /T4 12 Tf 50 640 Td (AB) Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(T1=t1, T2=t2, T3=t3, T4=t4),
        ExtGState=Dictionary(GS1=gs),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "ttencoding_2b.pdf", min_version="1.7")
