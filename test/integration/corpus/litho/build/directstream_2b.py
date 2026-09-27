#!/usr/bin/env python3
"""Build directstream_2b.pdf: streams written inside other objects, at PDF/A-2b.

ISO 32000-1, 7.3.8.1 makes every stream an indirect object. litho's parser
reads one written inline, as veraPDF's does, and every writer path wrote it
back where it found it, where qpdf reads the object holding it as damaged; a
compacted write packed it into an object stream, which ISO 32000-1, 7.5.7
forbids. No corpus file holds a stream inside another object, so no count
reached the writers. The page holds one in each place a copy meets one:

| Where the stream sits                                     | Shows |
| --------------------------------------------------------- | ----- |
| the page's indirect content stream, first in `/Contents`  | A     |
| the `/Contents` array, directly, second                   | B     |
| the page's `/XObject` resources, directly, as `/Fm0`      | AB    |
| `/Fm0`'s own `/XObject` resources, directly, as `/Fm1`    | BA    |

The direct content stream draws `/Fm0`, which draws `/Fm1`, and each stream
shows its own text in the conforming embedded TrueType of ttcmap_2b.pdf, so a
write that drops or garbles one loses text a mutation promises. The text paints
in the initial DeviceGray, with no output intent.

The page's /MediaBox also runs on past its four coordinates into a reference to
the indirect content stream, as two pdfjs files write one by accident. A page
copy resolved that reference into the /CropBox it wrote, which put the content
stream inline too.

pikepdf cannot write a stream inline: qpdf makes every stream indirect. The base
revision is saved by pikepdf, and a second revision is appended by hand, as an
incremental update rewriting the page, indexed by a cross-reference table of
its own.

See ../README.md for the authoritative expected firing set.
"""

import pathlib

import _truetype
from _common import Array, Dictionary, Name, Stream, attach_xmp, out_path, pikepdf, save, xmp_packet

LATIN = {0x41: "A", 0x42: "B"}

pdf = pikepdf.new()
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoDirectSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoDirectSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)


def text(shown, x, y):
    return b"BT /F1 24 Tf %d %d Td (%s) Tj ET\n" % (x, y, shown)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, text(b"A", 72, 700))),
    )
)
pdf.pages.append(pikepdf.Page(page))
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))

path = pathlib.Path(out_path("directstream_2b.pdf"))
save(pdf, "directstream_2b.pdf")

base = path.read_bytes()
reopened = pikepdf.open(str(path))
pages_num = reopened.Root.Pages.objgen[0]
page_num = reopened.pages[0].obj.objgen[0]
content_num = reopened.pages[0].obj.Contents.objgen[0]
font_num = reopened.pages[0].obj.Resources.Font.F1.objgen[0]
root_num = reopened.Root.objgen[0]
size = int(reopened.trailer.Size)
doc_id = bytes(reopened.trailer.ID[0]).hex().encode()
reopened.close()
startxref = base.rindex(b"startxref")
prev = int(base[startxref:].split()[1])


def inline(dictionary, data):
    """A stream written as a direct object, where ISO 32000-1, 7.3.8.1 forbids one."""
    entries = dictionary + b" " if dictionary else b""
    return b"<< %s/Length %d >>\nstream\n%s\nendstream" % (entries, len(data), data)


def form(bbox, resources, data):
    return inline(b"/Type /XObject /Subtype /Form /BBox [%s] /Resources << %s >>" % (bbox, resources), data)


font_res = b"/Font << /F1 %d 0 R >>" % font_num
inner = form(b"0 0 100 40", font_res, text(b"BA", 0, 10))
outer = form(
    b"0 0 300 100",
    font_res + b" /XObject << /Fm1 " + inner + b" >>",
    text(b"AB", 0, 60) + b"/Fm1 Do\n",
)
direct_content = inline(b"", b"q 1 0 0 1 72 400 cm /Fm0 Do Q\n" + text(b"B", 72, 600))
page_body = (
    b"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792 %d 0 R] /Resources << %s /XObject << /Fm0 %s >> >> "
    b"/Contents [%d 0 R %s] >>" % (pages_num, content_num, font_res, outer, content_num, direct_content)
)

out = bytearray(base)
page_offset = len(out)
out += b"%d 0 obj\n%s\nendobj\n" % (page_num, page_body)
xref_offset = len(out)
out += b"xref\n0 1\n0000000000 65535 f\r\n%d 1\n%010d 00000 n\r\n" % (page_num, page_offset)
out += b"trailer\n<< /Size %d /Root %d 0 R /Prev %d /ID [<%s> <%s>] >>\n" % (size, root_num, prev, doc_id, doc_id)
out += b"startxref\n%d\n%%%%EOF\n" % xref_offset
path.write_bytes(bytes(out))
print("wrote", path, len(out), "bytes")
