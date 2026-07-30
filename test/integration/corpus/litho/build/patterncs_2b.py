#!/usr/bin/env python3
"""Build patterncs_2b.pdf: pattern and appearance-stream colour at PDF/A-2b.

ISO 19005-2, 6.2.4.3 test 4 forbids DeviceGray the OutputIntent does not cover.
A tiling pattern and an annotation appearance stream both carry their own content
stream, and neither is invoked by a `Do`, so the question of what colour space
each begins in is decided by neither the page nor the object itself.

Measured against veraPDF, both begin in the default graphics state: a bare `scn`
inside either paints in the initial DeviceGray, and a colour space the page
selected before painting the pattern does not carry into it. That is the opposite
of a Form XObject, which does inherit the state at its `Do`, and it is why
[formcs_2b] cannot cover these two scopes.

Five objects sit in one file, two offending and three conforming, which is the
masking probe: an implementation that reads a bare `scn` as proof some space was
selected misses P0 and A0, and one that scans every declared pattern condemns P2,
which nothing paints. litho before this fixture did both at once.

| Object | Construct                                        | Rule        |
| ------ | ------------------------------------------------ | ----------- |
| P0     | tiling pattern, bare `scn`, painted              | `6.2.4.3/4` |
| P1     | tiling pattern selecting its own ICCBased space   | silent      |
| P2     | tiling pattern, DeviceCMYK, never painted         | silent      |
| A0     | appearance stream, bare `scn`                    | `6.2.4.3/4` |
| A1     | appearance stream selecting its own space         | silent      |

P2 is the control that matters most, and it is the one litho got wrong: the
pattern walk had no usage gate, so a pattern present in `/Resources/Pattern` was
scanned whether or not any `scn` named it. veraPDF is usage-driven here and stays
silent, so a declared-but-unpainted pattern was a false positive.

P1 and A1 are the over-suppression controls: each selects a permitted ICCBased
space before painting, so a firing against either means the scope's own `cs` was
ignored.

The two defects have to stay on different tests or they cancel. An earlier draft
gave P2 a DeviceGray fill and added a Form XObject painting DeviceCMYK: litho
before the fix then still reported both tests, missing P0 and A0 but scanning P2,
so the file agreed with veraPDF for the wrong reasons and proved nothing. P2 now
paints DeviceCMYK and no conforming object paints any, which leaves test 4
sourced only by P0 and A0 and test 3 reachable only by scanning P2.

The file declares no OutputIntent: test 4 is `gOutputCS != null`, so an
OutputIntent of any destination colour space would silence the clause outright.
The page paints its own text inside the ICCBased scope, because text drawn with
no colour space selected is itself a DeviceGray use and would report the clause
from the page, masking every object above.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(colour_space, components, tag):
    """A structurally minimal ICC profile veraPDF will parse without throwing."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = components
    return pdf.make_indirect(stream)


iccbased = pdf.make_indirect(Array([Name.ICCBased, icc_profile(b"RGB ", 3, b"ICCB")]))
own_cs = Dictionary(ColorSpace=Dictionary(CS0=iccbased))


def tiling_pattern(content):
    """One coloured tiling pattern carrying its own /Resources."""
    pat = Stream(pdf, content)
    pat.Type = Name.Pattern
    pat.PatternType = 1
    pat.PaintType = 1
    pat.TilingType = 1
    pat.BBox = Array([0, 0, 20, 20])
    pat.XStep = 20
    pat.YStep = 20
    pat.Resources = own_cs
    return pdf.make_indirect(pat)


def appearance(content):
    """One annotation appearance stream, which is a Form XObject."""
    ap = Stream(pdf, content)
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 20, 20])
    ap.Resources = own_cs
    return pdf.make_indirect(ap)


patterns = Dictionary(
    # A bare scn: nothing is inherited, so this paints in the initial DeviceGray.
    P0=tiling_pattern(b"0.5 scn 0 0 20 20 re f\n"),
    # Selects a permitted space of its own before painting.
    P1=tiling_pattern(b"/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n"),
    # Declared only. No scn names it, so it paints nothing and uses no colour.
    # Its fill is DeviceCMYK rather than DeviceGray so that scanning it by
    # mistake reports test 3, which nothing else in this file produces.
    P2=tiling_pattern(b"0 0 0 1 k 0 0 20 20 re f\n"),
)

annots = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Square,
                Rect=Array([400, 700, 420, 720]),
                F=4,
                AP=Dictionary(N=appearance(b"0.5 scn 0 0 20 20 re f\n")),
            )
        ),
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Square,
                Rect=Array([440, 700, 460, 720]),
                F=4,
                AP=Dictionary(
                    N=appearance(b"/CS0 cs 0.2 0.4 0.6 scn 0 0 20 20 re f\n")
                ),
            )
        ),
    ]
)

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

content = (
    b"q /CS0 cs 0.2 0.4 0.6 scn\n"
    # The page text sits inside the ICCBased scope so the page adds no DeviceGray.
    b"  BT /F1 12 Tf 50 740 Td (pattern and appearance colour) Tj ET\n"
    b"Q\n"
    # P0 and P1 are painted. P2 is never named, so it stays unused.
    b"q /Pattern cs /P0 scn 60 620 100 60 re f Q\n"
    b"q /Pattern cs /P1 scn 200 620 100 60 re f Q\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            ColorSpace=Dictionary(CS0=iccbased),
            Pattern=patterns,
        ),
        Contents=Stream(pdf, content),
        Annots=annots,
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata.
save(pdf, "patterncs_2b.pdf", min_version="1.7")
