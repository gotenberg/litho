#!/usr/bin/env python3
"""Build cmaps_2b.pdf: CMap embedding and consistency under PDF/A-2b.

The coverage scan showed ISO 19005-2, 6.2.11.3.3 unreached, and it was the
largest remaining cluster shared by all four part-2 and part-3 profiles. It
governs the CMaps a Type 0 font uses: which may be named rather than embedded,
whether an embedded CMap agrees with its own dictionary, and what an embedded
CMap may pull in through `/UseCMap`.

Three defects sit in three sibling fonts, all drawn on the page, which is the
masking probe: a check that reports the first offending CMap and stops reports
one rule where three are due. Two conforming fonts sit beside them, one naming
a predefined CMap and one embedding a CMap that agrees with itself and pulls in
only a predefined base.

The `/UseCMap` of font C references an embedded CMap stream rather than naming
one. That is not decoration: a `/UseCMap` naming a CMap veraPDF cannot resolve
makes it throw, abandoning the run, so the reference has to point at a real
stream. See ../FIXTURES.md.

The descendant CIDFonts carry no font program, which is a defect of a different
clause entirely, so a CMap verdict is visibly separate from the embedding one.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

IDENTITY_SYSTEM_INFO = Dictionary(
    Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0
)


def cmap_program(name, wmode):
    """A minimal but well-formed CMap program."""
    return (
        b"%!PS-Adobe-3.0 Resource-CMap\n"
        b"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n"
        b"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def "
        b"/Ordering (Identity) def /Supplement 0 def end def\n"
        b"/CMapName /" + name.encode() + b" def\n/CMapType 1 def\n"
        b"/WMode " + str(wmode).encode() + b" def\n"
        b"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
        b"1 begincidrange\n<0000> <FFFF> 0\nendcidrange\n"
        b"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
    )


def embedded_cmap(name, *, program_wmode=0, dict_wmode=0, use_cmap=None):
    """One embedded CMap stream."""
    s = Stream(pdf, cmap_program(name, program_wmode))
    s.Type = Name.CMap
    s.CMapName = Name("/" + name)
    s.CIDSystemInfo = IDENTITY_SYSTEM_INFO
    s.WMode = dict_wmode
    if use_cmap is not None:
        s.UseCMap = use_cmap
    return pdf.make_indirect(s)


def type0(name, encoding):
    """A Type 0 font whose descendant carries no font program."""
    descriptor = pdf.make_indirect(
        Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name("/" + name),
            Flags=4,
            ItalicAngle=0,
            Ascent=880,
            Descent=-120,
            CapHeight=700,
            StemV=80,
            FontBBox=Array([0, -120, 1000, 880]),
        )
    )
    descendant = pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.CIDFontType0,
            BaseFont=Name("/" + name),
            CIDSystemInfo=IDENTITY_SYSTEM_INFO,
            FontDescriptor=descriptor,
            DW=1000,
        )
    )
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.Type0,
            BaseFont=Name("/" + name),
            Encoding=encoding,
            DescendantFonts=Array([descendant]),
        )
    )


fonts = {
    # A names a CMap that is neither one of the predefined set nor embedded.
    "T1": type0("LithoNamedCMap", Name("/LithoCustomCMap")),
    # B embeds a CMap whose dictionary /WMode contradicts its program.
    "T2": type0("LithoWModeSplit", embedded_cmap("LithoWModeCMap", program_wmode=0, dict_wmode=1)),
    # C embeds a CMap that pulls in another embedded CMap whose name is not one
    # of the predefined set.
    "T3": type0(
        "LithoChained",
        embedded_cmap(
            "LithoOuterCMap",
            use_cmap=embedded_cmap("LithoInnerCMap"),
        ),
    ),
    # D and E conform. D names a predefined CMap; E embeds one that agrees with
    # its own dictionary and pulls in a predefined base by name.
    "T4": type0("LithoIdentity", Name("/Identity-H")),
    "T5": type0(
        "LithoEmbeddedOK",
        embedded_cmap("LithoConformingCMap", use_cmap=Name("/Identity-H")),
    ),
}

content = b"".join(
    b"BT /%s 12 Tf 50 %d Td <0041> Tj ET\n" % (key.encode(), 720 - i * 20)
    for i, key in enumerate(sorted(fonts))
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(**fonts)),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "cmaps_2b.pdf", min_version="1.7")
