#!/usr/bin/env python3
"""Build metafilter_1b.pdf: a filtered metadata stream, and one that may be.

| Construct                                          | Rule            |
| -------------------------------------------------- | --------------- |
| the catalog's XMP packet carries `/Filter`         | `6.7.2/2`       |

ISO 19005-1, 6.7.2 test 2 reads

    isCatalogMetadata == false || Filter == null

so the prohibition is on the CATALOG's packet alone. A metadata stream hanging
off anything else may be filtered. The file carries both, encoded identically:
the catalog's packet offends, and a page's packet with the same `/Filter
/FlateDecode` and the same compressed bytes is the control. A checker matching
on the filter alone rather than on whose packet it is condemns both, and a
checker that skipped metadata streams entirely condemns neither, so the pair
separates the two failure modes a single stream cannot.

Both packets are well-formed XML and the catalog's carries a correct PDF/A-1b
claim, so the filter is the only thing either can answer for.

qpdf strips `/Filter` off a metadata stream on every save path tried, including
with `preserve_pdfa` and `fix_metadata_version` off, so the construct cannot
survive pikepdf. Each stream is therefore planted after the save, its dictionary
padded with spaces so the object keeps its byte length and no cross-reference
offset moves. Padding after `endobj` instead would cost a `6.1.8/1`.

See ../README.md for the authoritative expected firing set.
"""

import re
import zlib

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save, xmp_packet

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
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (metadata stream filters) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

CATALOG_PACKET = xmp_packet(part=1, conformance="B")

# The control's packet carries no PDF/A claim of its own: a second claim would
# be a defect in itself and would stop the filter answering alone.
PAGE_PACKET = xmp_packet()

catalog_meta = Stream(pdf, CATALOG_PACKET)
catalog_meta.Type = Name.Metadata
catalog_meta.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(catalog_meta)

page_meta = Stream(pdf, PAGE_PACKET)
page_meta.Type = Name.Metadata
page_meta.Subtype = Name.XML
page.Metadata = pdf.make_indirect(page_meta)

# Deliberately no /OutputIntents, an independent defect that keeps the file's
# verdict from resting on the metadata streams alone.
save(pdf, "metafilter_1b.pdf", min_version="1.7")

path = out_path("metafilter_1b.pdf")
with open(path, "rb") as fh:
    data = fh.read()

OBJECT = re.compile(
    rb"(\d+ 0 obj\n)(<<[^<>]*?/Type /Metadata[^<>]*?>>)(\nstream\n)(.*?)(\nendstream\nendobj\n)",
    re.S,
)


def plant_filter(match):
    """Rewrite one metadata object as Flate data, keeping its byte length."""
    header, old_dict, open_kw, payload, close_kw = match.groups()
    encoded = zlib.compress(payload)
    new_dict = b"<< /Subtype /XML /Type /Metadata /Filter /FlateDecode /Length %d" % len(encoded)
    slack = len(old_dict) + len(payload) - len(new_dict) - len(b" >>") - len(encoded)
    if slack < 0:
        raise SystemExit("compressed packet does not fit the original object")
    return header + new_dict + b" " * slack + b" >>" + open_kw + encoded + close_kw


patched, count = OBJECT.subn(plant_filter, data)
if count != 2:
    raise SystemExit("expected 2 metadata objects, patched %d" % count)
if len(patched) != len(data):
    raise SystemExit("patch moved bytes: %d -> %d" % (len(data), len(patched)))

with open(path, "wb") as fh:
    fh.write(patched)
print("planted /Filter on", count, "metadata streams in", path)
