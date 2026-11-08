#!/usr/bin/env python3
"""Build xref_1b.pdf: cross reference table formatting under PDF/A-1b.

The coverage scan showed ISO 19005-1, 6.1.4 unreached. Two of its three tests
constrain the bytes of the cross reference table itself: the `xref` keyword and
the subsection header are separated by exactly one EOL marker, and the header's
starting object number and count by exactly one SPACE.

No writer emits either defect, so both are planted as bytes after the save. The
patch stays inside the cross reference section, which is the one region whose
own offset nothing points into: `startxref` records where the table begins, and
that does not move. Object offsets are all earlier in the file and unaffected.

Both defects sit on the same header line on purpose, and that is the point of
the fixture. They are separate tests of one clause, so a scanner that stops at
the first one it finds reports one rule where two are due. litho did exactly
that: the surplus EOL left its parser positioned on the extra byte, the
subsection header scan then read no digits and stopped, and the spacing defect
went unreported. Only the pair reveals it; either defect alone agreed.

Test 3 forbids a cross reference stream, which cannot coexist with a table, so
it belongs to a file of its own and is not planted here.

Beyond the clause the file carries the usual independent defects, no XMP packet
and no output intent, so a table verdict is visibly separate from them.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import pathlib
import re

from _common import Array, Dictionary, Name, Stream, out_path, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(
            pdf, b"BT /F1 12 Tf 50 700 Td (cross reference table) Tj ET\n"
        ),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "xref_1b.pdf", min_version="1.4")

path = out_path("xref_1b.pdf")
raw = pathlib.Path(path).read_bytes()

# `xref\n0 6\n` becomes `xref\n\n0  6\n`: a second EOL marker after the keyword
# (test 2) and a second space inside the header (test 1). Both bytes are added
# inside the table, after the offset `startxref` records.
patched, count = re.subn(rb"(\nxref)\n(\d+) (\d+)", rb"\1\n\n\2  \3", raw, count=1)
if count != 1:
    raise SystemExit("xref_1b: expected exactly one cross reference subsection header")
pathlib.Path(path).write_bytes(patched)
print("patched the subsection header of", path)
