#!/usr/bin/env python3
"""Build xmpsyntax_1b.pdf: three defects, one per layer of the file.

| Construct                                          | Rule            |
| -------------------------------------------------- | --------------- |
| a header that is not `%PDF-` followed by `n.n`     | `6.1.2/1`       |
| a cross-reference stream instead of a table        | `6.1.4/3`       |
| a raw `<` inside the XMP packet's character data   | `6.7.9/1`       |

The three sit in three different layers: the first eight bytes of the file, the
format of its cross-reference section, and the XML inside its metadata stream.
That separation is the point. A reader that gave up on the malformed header
would never reach the cross-reference section, and one that gave up there would
never reach the metadata, so each rule firing is evidence that the reader kept
going past the layer above it. A file carrying only one of the three could not
show that.

`6.1.2/1` reads `headerOffset == 0 && /%PDF-\\d\\.\\d/.test(header)`. Turning
`%PDF-1.7` into `%PDF-17.` breaks the pattern while leaving the header at offset
zero and every byte after it exactly where it was, so the cross-reference
offsets stay valid and the file still parses. Prepending junk instead would move
every offset in the file, which makes veraPDF fail to parse rather than report.

The raw `<` also costs the file its PDF/A identification, because the packet
carrying it cannot be parsed, so `6.7.11/1` fires alongside. That is the
construct working, not a second defect: the identification lives inside the
packet the `<` breaks.

pikepdf regenerates the XMP packet on save unless both `preserve_pdfa` and
`fix_metadata_version` are off, which silently repairs the planted `<`. The
build asserts the byte survived rather than trusting the flags.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    out_path,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def page(text):
    """One conforming page. The defects live outside the page tree."""
    made = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(Font=Dictionary(F1=font)),
            Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (%s) Tj ET\n" % text),
        )
    )
    pdf.pages.append(pikepdf.Page(made))
    return made


# Two pages, both clean. They exist so a reader that recovers from the header
# and cross-reference defects has something whole to report on, which is what
# makes the absence of page-level violations meaningful.
page(b"header, cross-reference and XMP syntax")
page(b"a second page, carrying no defect of its own")

# 6.7.9 t1: a raw '<' where XML requires an entity. The packet stops being
# parseable, and with it the PDF/A identification it carries.
PACKET = xmp_packet(part=1, conformance="B").replace(
    b"litho corpus fixture", b"litho < corpus fixture"
)

metadata = Stream(pdf, PACKET)
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

# 6.1.4 t3: object streams force a cross-reference stream, which ISO 19005-1
# forbids outright. Deliberately no /OutputIntents, an independent defect that
# keeps the file-level verdict from resting on the three constructs alone.
save(
    pdf,
    "xmpsyntax_1b.pdf",
    min_version="1.7",
    object_stream_mode=pikepdf.ObjectStreamMode.generate,
    preserve_pdfa=False,
    fix_metadata_version=False,
)

path = out_path("xmpsyntax_1b.pdf")
with open(path, "rb") as fh:
    data = bytearray(fh.read())

if b"litho < corpus fixture" not in data:
    raise SystemExit("the planted '<' did not survive the save")

# 6.1.2 t1: break the version pattern without moving a byte, so every
# cross-reference offset written above stays correct.
if bytes(data[:8]) != b"%PDF-1.7":
    raise SystemExit("unexpected header %r" % bytes(data[:8]))
data[:8] = b"%PDF-17."

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("patched header of", path)
