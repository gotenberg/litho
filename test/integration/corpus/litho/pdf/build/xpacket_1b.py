#!/usr/bin/env python3
"""Build xpacket_1b.pdf: the attributes an XMP packet header may not carry.

| Construct                                          | Rule            |
| -------------------------------------------------- | --------------- |
| `bytes` on the catalog packet's xpacket header     | `6.7.5/1`       |
| `encoding` on the same header                      | `6.7.5/2`       |

ISO 19005-1, 6.7.5 reads `bytes == null` and `encoding == null` against
veraPDF's XMPPackage. Both attributes ride in the `<?xpacket ...?>` processing
instruction, not in any PDF dictionary, so reaching the clause means writing the
packet header rather than setting a key.

The two are carried together and answered separately, so a checker that
recognised one attribute and not the other is visible as a missing rule rather
than hidden behind its neighbour.

A second packet hangs off the page carrying the same two attributes. It is the
control: measured against veraPDF, a packet outside the catalog answers for
neither test, so a checker matching any packet header rather than the document's
own is visible as an extra firing. That scope was measured, not assumed.

pikepdf regenerates the packet on save unless both `preserve_pdfa` and
`fix_metadata_version` are off, and the regenerated header carries neither
attribute. The build asserts both survived rather than trusting the flags.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save, xmp_packet

pdf = pikepdf.new()

PLAIN_HEADER = b'<?xpacket begin="\xef\xbb\xbf" id="W5M0MpCehiHzreSzNTczkc9d"?>'
OFFENDING_HEADER = (
    b'<?xpacket begin="\xef\xbb\xbf" id="W5M0MpCehiHzreSzNTczkc9d"'
    b' bytes="2048" encoding="UTF-8"?>'
)


def with_offending_header(packet):
    """Rewrite a packet's xpacket header to carry bytes and encoding."""
    if PLAIN_HEADER not in packet:
        raise SystemExit("xmp_packet no longer emits the expected header")
    return packet.replace(PLAIN_HEADER, OFFENDING_HEADER)


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
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (xpacket header attributes) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

catalog_meta = Stream(pdf, with_offending_header(xmp_packet(part=1, conformance="B")))
catalog_meta.Type = Name.Metadata
catalog_meta.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(catalog_meta)

# The control. It carries no PDF/A claim of its own, so a second claim cannot be
# what keeps it silent: only its position outside the catalog can.
page_meta = Stream(pdf, with_offending_header(xmp_packet()))
page_meta.Type = Name.Metadata
page_meta.Subtype = Name.XML
page.Metadata = pdf.make_indirect(page_meta)

# Deliberately no /OutputIntents, an independent defect that keeps the file's
# verdict from resting on the packet headers alone.
save(
    pdf,
    "xpacket_1b.pdf",
    min_version="1.7",
    preserve_pdfa=False,
    fix_metadata_version=False,
)

with open(out_path("xpacket_1b.pdf"), "rb") as fh:
    data = fh.read()
if data.count(OFFENDING_HEADER) != 2:
    raise SystemExit("the planted xpacket headers did not survive the save")
print("both xpacket headers survived")
