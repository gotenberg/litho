#!/usr/bin/env python3
"""Build xrefstm_2b.pdf: the trailer a cross-reference stream carries.

ISO 32000-1, 7.5.8.2 has a cross-reference stream's dictionary hold the file's
trailer beside the entries that describe the stream, and veraPDF builds the
trailer from the entries ISO 32000-1, Table 15 defines: /Size, /Prev, /Root,
/Encrypt, /Info and /ID. The dictionary is an indirect object, which the object
sweep judges whole, so a value in a trailer entry is judged twice, and a
document information dictionary written directly there three times, while an
entry Table 15 does not define is judged once. An older section's stream is an
indirect object like any other, judged once. 922 corpus files end in a
cross-reference stream, and not one holds a defect in its trailer entries, so no
count reached the reading.

qpdf writes the first section, a cross-reference stream whose trailer holds a
direct /Info. The second section is appended by hand, as an incremental update
rewriting the page's content stream, indexed by a stream of its own:

| Where                                      | Value                         | Rule at PDF/A-2b | Count |
| ------------------------------------------ | ----------------------------- | ---------------- | ----- |
| newest `/ID`, first element                | 31 hex digits                 | `6.1.6/1`        | 2     |
| newest direct `/Info`, `/Title`            | `<414>`                       | `6.1.6/1`        | 3     |
| newest direct `/Info`, `/Producer`         | `<41G1>`                      | `6.1.6/2`        | 3     |
| newest `/Foo`, no Table 15 entry           | `<414>`                       | `6.1.6/1`        | 1     |
| older direct `/Info`, `/Producer`          | `<414>`                       | `6.1.6/1`        | 1     |
| newest direct `/Info`, `/Count`            | 2147483648                    | `6.1.13/1`       | 3     |
| newest `/Big`, no Table 15 entry           | 2147483648                    | `6.1.13/1`       | 1     |
| older direct `/Info`, `/Count`             | 2147483648                    | `6.1.13/1`       | 1     |
| newest direct `/Info`, `/Subject`          | 32768 bytes                   | `6.1.13/3`       | 3     |
| newest direct `/Info`, a key               | 200 bytes                     | `6.1.13/4`       | 3     |
| newest, a key of the stream dictionary     | 200 bytes                     | `6.1.13/4`       | 1     |
| newest direct `/Info`, `/Width`            | 40000.5                       | PDF/A-1 `6.1.12/2` | 3   |

Each rule's count is the sum of its rows. The real past 32767 is a limit of
PDF/A-1 alone, which is where it answers. The newest /Size stays in range:
veraPDF judges an out-of-range /Size twice like any trailer entry, but qpdf
then refuses the file outright, and a fixture viewers cannot open tests
nothing a user sees.

The first section's direct /Info holds `(QQQQQ)`, patched as bytes into
`<414>  ` after the save, since qpdf writes every string it knows as a literal
of its own choosing; the width is kept, so no offset moves.

See ../README.md for the authoritative expected firing set.
"""

import pathlib
import struct

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf

LONG_KEY = b"K" * 200
BIG_INT = 2147483648

pdf = pikepdf.new()
content = Stream(pdf, b"0 0 m 10 10 l n\n")
page = pdf.make_indirect(
    Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 200, 100]), Resources=Dictionary(), Contents=content)
)
pdf.pages.append(pikepdf.Page(page))
pdf.trailer.Info = Dictionary(Producer=String("QQQQQ"), Count=BIG_INT)

# Deliberately no /Metadata and no output intent.
path = pathlib.Path(out_path("xrefstm_2b.pdf"))
pdf.save(
    str(path),
    min_version="1.7",
    deterministic_id=True,
    compress_streams=False,
    stream_decode_level=pikepdf.StreamDecodeLevel.none,
    object_stream_mode=pikepdf.ObjectStreamMode.generate,
)

base = path.read_bytes()
if base.count(b"(QQQQQ)") != 1 or b"/Type /XRef" not in base:
    raise SystemExit("xrefstm_2b: qpdf did not write one cross-reference stream holding the direct /Info")
base = base.replace(b"(QQQQQ)", b"<414>  ")

startxref = base.rindex(b"startxref")
prev = int(base[startxref:].split()[1])
reopened = pikepdf.open(str(path))
root_num = reopened.Root.objgen[0]
content_num = reopened.pages[0].obj.Contents.objgen[0]
size = int(reopened.trailer.Size)
reopened.close()
xref_num = size


def row(offset):
    """One cross-reference stream entry under /W [1 4 2]: an in-use object."""
    return struct.pack(">BIH", 1, offset, 0)


out = bytearray(base)
content_offset = len(out)
body = b"0 0 m 20 20 l n\n"
out += b"%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n" % (content_num, len(body), body)
xref_offset = len(out)
rows = row(content_offset) + row(xref_offset)
info = b"<< /Title <414> /Producer <41G1> /Count %d /Subject (%s) /%s (x) /Width 40000.5 >>" % (
    BIG_INT, b"x" * 32768, LONG_KEY)
dictionary = (
    b"<< /Type /XRef /Size %d /Index [%d 1 %d 1] /W [1 4 2] /Root %d 0 R /Prev %d "
    b"/ID [<a11ceed0a11ceed0a11ceed0a11ceed> <b0bb1e5b0bb1e5b0bb1e5b0bb1e5b0bb>] "
    b"/Info %s /Foo <414> /Big %d /%s 1 /Length %d >>"
) % (xref_num + 1, content_num, xref_num, root_num, prev, info, BIG_INT, LONG_KEY, len(rows))
out += b"%d 0 obj\n%s\nstream\n" % (xref_num, dictionary) + rows + b"\nendstream\nendobj\n"
out += b"startxref\n%d\n%%%%EOF\n" % xref_offset
path.write_bytes(bytes(out))
print("wrote", path, len(out), "bytes")
