#!/usr/bin/env python3
"""Build limits_2b.pdf: implementation limits from ISO 19005-2, 6.1.13.

The coverage scan showed 6.1.13 as the second largest unreached cluster. Its
tests are scattered across object types rather than gathered in one
dictionary, which is what makes them worth planting together: each defect
lives in a different part of the file, so a check that stops at the first
limit breach reports one rule where four are due.

Not every test in the clause is reachable this way. `t7` needs more than
8388607 indirect objects and `t10` needs a CMap with an out-of-range CID,
neither of which belongs in a small fixture. `t1`, `t2` and `t5` need numbers
a writer will not emit: pikepdf normalises a denormal such as 1e-40 to plain
`0`, which `t5` explicitly permits, so planting one through this builder is
not possible. Those stay uncovered deliberately.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# t4: a name longer than 127 bytes, used as a resource key so it is reachable
# from the page rather than orphaned.
long_name = "L" + "o" * 200 + "ng"

# t9: DeviceN admits at most 32 colorants; this one names 33.
colorants = [Name("/Ink%02d" % i) for i in range(33)]
tint = Stream(pdf, b"{ " + b"pop " * 32 + b"dup dup }")
tint.FunctionType = 4
tint.Domain = Array([0, 1] * 33)
tint.Range = Array([0, 1, 0, 1, 0, 1])
devicen = Array(
    [Name.DeviceN, Array(colorants), Name.DeviceRGB, pdf.make_indirect(tint)]
)

# t8: the graphics state stack may nest 28 deep; this content nests 30.
nesting = b"q\n" * 30 + b"0 0 0 rg 50 700 40 20 re f\n" + b"Q\n" * 30

content = nesting + (
    b"q /" + long_name.encode("ascii") + b" cs 0.5 sc 50 620 40 20 re f Q\n"
    b"BT /F1 12 Tf 50 580 Td (implementation limits) Tj ET\n"
)

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(F1=font),
        ColorSpace=Dictionary(**{long_name: devicen}),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# t3: a string of 40000 bytes, well past the 32767 limit. It sits on an
# annotation so it is reachable and is not confused with document metadata.
pdf.pages[0].Annots = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Square,
                Rect=Array([400, 700, 500, 750]),
                F=4,
                AP=Dictionary(N=Stream(pdf, b"")),
                Contents=String("s" * 40000),
            )
        )
    ]
)

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "limits_2b.pdf", min_version="1.7")
