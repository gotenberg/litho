#!/usr/bin/env python3
"""Build redactvectors_4.pdf: copies of page text no comparison of bytes finds.

Redaction verifies its output by looking for every removed string in what a
rewrite writes. Four copies of page 1's paragraph sit where that comparison
cannot see them, and before, redacting page 1 wrote every one out and reported
success:

* The catalog's /OpenAction and page 1's /AA /O are JavaScript actions that
  build the paragraph at run time from two pieces (ISO 32000-1, 12.6.4.16).
* The catalog's /Names /JavaScript tree runs a script held in a text stream,
  UTF-16BE behind its byte order mark (7.9.3), which interleaves every letter
  with a zero byte.
* Page 1's /Metadata is an XMP packet carrying a page thumbnail as base64
  (14.3.2; xmpGImg:image), here bytes standing in for its pixels.
* The catalog's /PieceInfo holds private data (14.5, Table 368): a stream with
  no filter whose bytes are a zlib stream of the paragraph, an application's
  own compressed format.

Redaction now removes every script, the redacted page's /Metadata and the
catalog's private data, keeping the /PieceInfo dictionary and its
/LastModified.

Two attachments hold no copy and must stay: a zip archive and a PDF whose
content stream is Flate-compressed, each about page two. The scan reads into
both and finds nothing.

Page 2 draws text no redaction of page 1 touches.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import base64
import io
import zipfile
import zlib

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
resources = Dictionary(Font=Dictionary(F1=font))

WORDS = b"the secret plan"

page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(pdf, b"BT /F1 12 Tf 72 700 Td (" + WORDS + b") Tj ET\n"),
    )
)
page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=resources,
        Contents=Stream(pdf, b"BT /F1 12 Tf 72 700 Td (page two) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page1))
pdf.pages.append(pikepdf.Page(page2))

SPLIT_SCRIPT = "var plan = 'the sec' + 'ret plan';"


def script():
    """A JavaScript action building the paragraph from two pieces."""
    return Dictionary(S=Name.JavaScript, JS=String(SPLIT_SCRIPT))


pdf.Root.OpenAction = script()
pdf.pages[0].obj.AA = Dictionary(O=script())

wide = Stream(pdf, b"\xfe\xff" + "app.alert('the secret plan');".encode("utf-16-be"))
pdf.Root.Names = Dictionary(
    JavaScript=Dictionary(
        Names=Array([String("init"), pdf.make_indirect(Dictionary(S=Name.JavaScript, JS=wide))])
    )
)

thumbnail = base64.b64encode(b"thumbnail pixels of page one as it was").decode()
xmp = Stream(
    pdf,
    (
        '<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF '
        'xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
        '<rdf:Description rdf:about="" xmlns:xmpGImg="http://ns.adobe.com/xap/1.0/g/img/">'
        "<xmpGImg:image>" + thumbnail + "</xmpGImg:image>"
        "</rdf:Description></rdf:RDF></x:xmpmeta>"
    ).encode(),
)
xmp.Type = Name.Metadata
xmp.Subtype = Name.XML
pdf.pages[0].obj.Metadata = pdf.make_indirect(xmp)

# No /Filter: the bytes are the application's own compressed format, which no
# decode reads, so the stored zlib stream hides the paragraph from the scan.
private = Stream(pdf, zlib.compress(WORDS * 8, 9))
pdf.Root.PieceInfo = Dictionary(
    LithoFixture=Dictionary(LastModified=String("D:20260101000000Z"), Private=pdf.make_indirect(private))
)


def notes_zip():
    """A zip archive of one deflated member about page two, deterministic."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as archive:
        info = zipfile.ZipInfo("notes.txt", date_time=(2026, 1, 1, 0, 0, 0))
        info.compress_type = zipfile.ZIP_DEFLATED
        archive.writestr(info, b"notes about page two, reviewed.\n" * 32)
    return buf.getvalue()


def notes_pdf():
    """A one-page PDF whose Flate-compressed content is about page two."""
    content = zlib.compress(b"BT /F1 12 Tf 72 700 Td (notes about page two) Tj ET\n" * 8, 9)
    objects = [
        b"<< /Type /Catalog /Pages 2 0 R >>",
        b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
        b"<< /Filter /FlateDecode /Length " + str(len(content)).encode() + b" >>\nstream\n" + content + b"\nendstream",
    ]
    out = bytearray(b"%PDF-1.7\n")
    offsets = []
    for number, body in enumerate(objects, start=1):
        offsets.append(len(out))
        out += str(number).encode() + b" 0 obj\n" + body + b"\nendobj\n"
    xref = len(out)
    out += b"xref\n0 5\n0000000000 65535 f\r\n"
    for offset in offsets:
        out += b"%010d 00000 n\r\n" % offset
    out += b"trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n" % xref
    return bytes(out)


def filespec(name, data, subtype):
    """A file specification embedding data as name."""
    stream = Stream(pdf, data)
    stream.Type = Name.EmbeddedFile
    stream.Subtype = Name(subtype)
    stream = pdf.make_indirect(stream)
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Filespec,
            F=String(name),
            UF=String(name),
            AFRelationship=Name.Supplement,
            EF=Dictionary(F=stream, UF=stream),
        )
    )


notes = filespec("notes.zip", notes_zip(), "/application/zip")
draft = filespec("draft.pdf", notes_pdf(), "/application/pdf")
pdf.Root.Names.EmbeddedFiles = Dictionary(Names=Array([String("draft.pdf"), draft, String("notes.zip"), notes]))
pdf.Root.AF = Array([draft, notes])

save(pdf, "redactvectors_4.pdf", min_version="2.0")
