#!/usr/bin/env python3
"""Build danglingstr_ua1.pdf: an untagged page whose content stream ends inside
a literal string that never terminates, at PDF/UA-1.

A literal string runs until its balancing right parenthesis (ISO 32000-1,
7.3.4.2), so one the stream never closes swallows every byte after it. When
remediation auto-tags the page it wraps the trailing content in an /Artifact
sequence, and an EMC appended after the last byte is lexed as string bytes,
not as the operator, which leaves the sequence open. ISO 32000-1, 14.6 makes no
marked-content sequence of one that never ends, so the remediated output
failed ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 on veraPDF. No file of the
litho or veraPDF corpora reaches it; the pdfjs file that does is not
remediated by the gate.

The page shows a line of text and strokes a rule, then opens the string
"(dangling ( string", whose nested parenthesis leaves it two levels deep at
the end of the stream. Neither tool reports the string as a syntax error, and
it paints nothing, since no operator follows it.

The document is otherwise ready for PDF/UA-1 but for its tagging: /Lang,
DisplayDocTitle, an XMP packet carrying dc:title and the pdfuaid claim, and the
conforming embedded TrueType of baretree_2b.pdf. So the input fails only the
tagging clauses, and auto-tagging is what remediation has to get right.

See ../README.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoBareSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoBareSans"),
        FirstChar=0x41,
        LastChar=0x42,
        Widths=Array([_truetype.ADVANCE_WIDTH] * 2),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)

content = (
    b"BT /F1 24 Tf 20 100 Td (AB) Tj ET\n"
    b"20 90 m 180 90 l S\n"
    b"(dangling ( string"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, content)),
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# Deliberately no /StructTreeRoot and no /MarkInfo: the page is untagged.
attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "danglingstr_ua1.pdf", min_version="1.7", fix_metadata_version=False)
