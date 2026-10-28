#!/usr/bin/env python3
"""Build limits_1b.pdf: implementation limits from ISO 19005-1, 6.1.12.

Part 1 and part 2 cap different things, so `limits_2b.pdf` never reaches this
clause. Its 40000-byte string breaks the part-2 cap of 32767 and sits well
inside the part-1 cap of 65535, and the array and dictionary caps exist in
part 1 only. A coverage scan therefore still showed `t1`, `t2`, `t3`, `t5`,
`t6`, `t7` and `t10` unreached.

Every breach lives in a different indirect object, and every one of them is
reachable from the page: four sibling entries of the page-piece dictionary,
two sibling annotations, two fonts. The page-piece dictionary is the masking
probe. A check that reports the first breach in that bag and stops reports one
rule where four are due.

Each breach is paired with a control at exactly the limit, in its own object,
so an off-by-one in either direction shows as a wrong firing count rather than
as silence:

    2147483648, -2147483649   against   2147483647, -2147483648
    32767.5, -32767.5         against   32767.0, -32767.0
    a 65536-byte string       against   a 65535-byte string
    an 8192-element array     against   an 8191-element array
    a 4096-entry dictionary   against   a 4095-entry dictionary
    a maximal CID of 65536    against   a maximal CID of 65535

PDF defines no standard dictionary that carries an arbitrary number, so the
numeric and container probes sit in the page-piece dictionary, which
ISO 32000-1, 14.5 reserves for private application data. ISO 32000-1,
Table 30 then requires the page `/LastModified` that accompanies it.

`t7` stays unreached on purpose. It needs more than 8388607 indirect objects,
whose cross-reference table alone runs to 168 MB at 20 bytes an entry.

The overflowing CMap range is one the content never renders. A rendered CID
above 65535 additionally trips litho's part-1 glyph-presence check on a font
that embeds no program, which veraPDF exempts, and this fixture is about
implementation limits rather than that divergence.

Deliberately no /Metadata and no /OutputIntents, as in `limits_2b.pdf`.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from decimal import Decimal

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

# t1: the first integer past each end of the 32-bit range, against the two
# values that sit on it.
ints_over = pdf.make_indirect(Dictionary(Hi=2147483648, Lo=-2147483649))
ints_edge = pdf.make_indirect(Dictionary(Hi=2147483647, Lo=-2147483648))

# t2: the same, half a unit past each end. Decimal keeps the token verbatim.
# pikepdf writes a plain Python float of 32767.0 as the integer token `32767`,
# which is a CosInteger and tests nothing.
reals_over = pdf.make_indirect(
    Dictionary(Hi=Decimal("32767.5"), Lo=Decimal("-32767.5"))
)
reals_edge = pdf.make_indirect(
    Dictionary(Hi=Decimal("32767.0"), Lo=Decimal("-32767.0"))
)

# t5 and t6: one element and one entry past the container limits.
array_over = pdf.make_indirect(Array([0] * 8192))
array_edge = pdf.make_indirect(Array([0] * 8191))
dict_over = pdf.make_indirect(Dictionary(**{"K%04d" % i: 0 for i in range(4096)}))
dict_edge = pdf.make_indirect(Dictionary(**{"K%04d" % i: 0 for i in range(4095)}))

MODIFIED = String("D:20240101000000+00'00'")


def probe(over, edge):
    """Pair one breach with its control in a page-piece data dictionary."""
    return Dictionary(LastModified=MODIFIED, Private=Array([over, edge]))


def annotation(y, text):
    """Build a conforming Square annotation carrying text in /Contents."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Square,
            Rect=Array([400, y, 500, y + 50]),
            F=4,
            AP=Dictionary(N=Stream(pdf, b"")),
            Contents=String(text),
        )
    )


# t3: the limit is a length below 65536, so 65536 bytes is the first failing
# one. Both annotations conform in every other respect, which keeps 6.5.3 out
# of the way.
string_over = annotation(700, "o" * 65536)
string_edge = annotation(620, "e" * 65535)


def cmap(name, base):
    """Embed a CMap whose second range is never rendered and ends at base+255.

    Codes <0000> to <00FF> carry the rendered text and stay on low CIDs. The
    maximal CID the file declares is therefore base+255, which is what
    ISO 19005-1, 6.1.12 caps, without the content ever selecting a glyph the
    descendant font cannot supply.
    """
    body = "\n".join(
        [
            "/CIDInit /ProcSet findresource begin",
            "12 dict begin",
            "begincmap",
            "/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) "
            "/Supplement 0 >> def",
            "/CMapName /%s def" % name,
            "/CMapType 1 def",
            "/WMode 0 def",
            "1 begincodespacerange",
            "<0000> <FFFF>",
            "endcodespacerange",
            "2 begincidrange",
            "<0000> <00FF> 1",
            "<F000> <F0FF> %d" % base,
            "endcidrange",
            "endcmap",
            "CMapName currentdict /CMap defineresource pop",
            "end",
            "end",
            "",
        ]
    )
    stream = Stream(pdf, body.encode("ascii"))
    stream.Type = Name.CMap
    stream.CMapName = Name("/" + name)
    stream.CIDSystemInfo = Dictionary(
        Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0
    )
    stream.WMode = 0
    return pdf.make_indirect(stream)


def composite_font(name, base):
    """Build a Type0 font over an embedded CMap and one descendant CIDFont."""
    descendant = pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.CIDFontType2,
            BaseFont=Name("/" + name),
            CIDSystemInfo=Dictionary(
                Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0
            ),
            DW=1000,
            CIDToGIDMap=Name.Identity,
            FontDescriptor=Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/" + name),
                Flags=4,
                FontBBox=Array([0, 0, 1000, 1000]),
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
            ),
        )
    )
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type0,
            BaseFont=Name("/" + name),
            Encoding=cmap(name + "-H", base),
            DescendantFonts=Array([descendant]),
        )
    )


# t10: 65281 puts the maximal CID at 65536, one past the cap; 65280 puts it at
# 65535, exactly on it.
font_over = composite_font("LithoOver", 65281)
font_edge = composite_font("LithoEdge", 65280)

content = (
    b"BT /F0 12 Tf 50 560 Td <0041> Tj ET\n"
    b"BT /F1 12 Tf 50 540 Td <0041> Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F0=font_over, F1=font_edge)),
    Contents=Stream(pdf, content),
    Annots=Array([string_over, string_edge]),
    LastModified=MODIFIED,
    PieceInfo=Dictionary(
        LithoInteger=probe(ints_over, ints_edge),
        LithoReal=probe(reals_over, reals_edge),
        LithoArray=probe(array_over, array_edge),
        LithoDict=probe(dict_over, dict_edge),
    ),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

save(pdf, "limits_1b.pdf", min_version="1.4")
