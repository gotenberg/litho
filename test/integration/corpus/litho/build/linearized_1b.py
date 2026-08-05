#!/usr/bin/env python3
"""Build linearized_1b.pdf: a linearized file whose two trailers disagree on /ID.

ISO 19005-1, 6.1.3 test 4 is the only rule in the corpus that reads more than
one trailer. It reads `(isLinearized != true) || lastID == null ||
(firstPageID == lastID)`: a linearized file states its `/ID` twice, once in the
first-page trailer at the top and once in the last trailer at the end, and the
two shall be identical. No fixture reached it, and a checker that keeps only
one trailer cannot answer it at all.

The two `/ID` arrays here are:

| Trailer    | `/ID`                          |
| ---------- | ------------------------------ |
| first page | `[<a11c...> <c0ff...>]`        |
| last       | `[<a11c...> <b0bb...>]`        |

Element 0 matches and element 1 does not, so only a check comparing the whole
array answers. Both arrays also hold two *different* strings, which is the
control: ISO 32000-1, 14.4 has element 1 change whenever the file is updated,
so a pair that differs within one trailer is conformant. A check that reads the
requirement as "the two entries of the `/ID` array shall be identical" fires on
the last trailer, which is the conforming one.

Test 1 of the same clause, `/ID` presence, stays silent: both trailers carry the
key and every element is a non-empty string. The 6.1.3 verdict is therefore
attributable to the comparison alone.

The three pages carry an unembedded Helvetica and paint in DeviceGray, and the
file has neither `/Metadata` nor an output intent, so the font, colour and
metadata clauses answer from three other parts of the specification. A checker
that stops at its first structural verdict reports one rule where four are due.

qpdf lays the file out, since linearization is a whole-file property no
builder can plant object by object. The `/ID` values are patched afterwards
against the bytes qpdf wrote: they are hex strings of a fixed width, so no
offset moves and the linearized layout survives.

See ../README.md for the authoritative expected firing set.
"""

import re

from _common import Array, Dictionary, Name, Stream, out_path, pikepdf

# Three 16-byte identifiers. Element 0 is shared, element 1 is not.
ID_SHARED = b"a11ceed0a11ceed0a11ceed0a11ceed0"
ID_LAST = b"b0bb1e5b0bb1e5b0bb1e5b0bb1e5b0bb"
ID_FIRST_PAGE = b"c0ffeec0ffeec0ffeec0ffeec0ffeec0"

pdf = pikepdf.new()

font = pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.Type1,
    BaseFont=Name("/Helvetica"), Encoding=Name.WinAnsiEncoding))

for i in range(3):
    page = pdf.make_indirect(Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (page %d) Tj ET\n" % i)))
    pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata and no output intent.
path = out_path("linearized_1b.pdf")
pdf.save(path, min_version="1.7", deterministic_id=True, compress_streams=False,
         stream_decode_level=pikepdf.StreamDecodeLevel.none, linearize=True)

data = bytearray(open(path, "rb").read())
if b"/Linearized" not in data:
    raise SystemExit("qpdf did not linearize the file, so 6.1.3 test 4 is out of scope")

matches = list(re.finditer(rb"/ID\s*\[\s*<([0-9a-fA-F]+)>\s*<([0-9a-fA-F]+)>\s*\]", bytes(data)))
if len(matches) != 2:
    raise SystemExit("expected exactly two trailers, found %d" % len(matches))

# The first match is the first-page trailer at the top of the file; the second
# is the last trailer. Every replacement keeps its width so no xref offset moves.
for match, second in ((matches[0], ID_FIRST_PAGE), (matches[1], ID_LAST)):
    for group, value in ((1, ID_SHARED), (2, second)):
        if match.end(group) - match.start(group) != len(value):
            raise SystemExit("patched /ID changes width, which would move an xref offset")
        data[match.start(group):match.end(group)] = value

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("wrote", path, len(data), "bytes")
