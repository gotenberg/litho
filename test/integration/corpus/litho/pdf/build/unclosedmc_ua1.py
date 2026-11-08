#!/usr/bin/env python3
"""Build unclosedmc_ua1.pdf: an artifact sequence the content stream never
closes, at PDF/UA-1.

ISO 32000-1, 14.6 delimits a marked-content sequence by its BMC or BDC and the
matching EMC, so content after a BMC no EMC closes lies in no sequence at all,
and ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 judge it as content neither
tagged nor artifacted. veraPDF reads it that way: its semantic content stream
makes everything after the last balanced EMC one unmarked group. litho counted
the open /Artifact as marking what followed it, so the page passed. No file of
the litho or veraPDF corpora reaches it; the pdfjs file that does is not
remediated by the gate.

The page holds a tagged paragraph, closed, as the control, then an /Artifact
BMC and a stroked rule with no EMC after it. The document is otherwise a
conforming PDF/UA-1 file: a structure tree whose /P reaches the paragraph,
/MarkInfo, /Lang, DisplayDocTitle, an XMP packet carrying dc:title and the
pdfuaid claim, and the conforming embedded TrueType of baretree_2b.pdf. So at
PDF/UA-1 and PDF/UA-2 the unclosed sequence is the one content finding.

See ../FIXTURES.md for the authoritative expected firing set.
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

# The paragraph's sequence closes; the artifact's never does, so the rule it
# strokes is in no sequence.
content = (
    b"/P <</MCID 0>> BDC BT /F1 24 Tf 20 100 Td (AB) Tj ET EMC\n"
    b"/Artifact BMC\n"
    b"20 90 m 180 90 l S\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=pdf.make_indirect(Stream(pdf, content)),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))
paragraph = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0))
document.K = Array([paragraph])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array([paragraph])])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

attach_xmp(pdf, xmp_packet(ua_part=1))
save(pdf, "unclosedmc_ua1.pdf", min_version="1.7", fix_metadata_version=False)
