#!/usr/bin/env python3
"""Build puatext_ua2.pdf: Private Use Area characters in the strings that describe.

ISO 14289-2 forbids a Private Use Area code point in text a consumer reads out,
because nothing outside the producing system knows what it means. The clause is
split across three veraPDF objects, so a file reaches each one only by putting a
PUA character in that exact position:

| Construct                                | Rule       |
| ---------------------------------------- | ---------- |
| `/ActualText` on a structure element     | `8.4.3/2`  |
| `/Alt` on a structure element            | `8.4.3/3`  |
| an outline item `/Title`                 | `8.6/1`    |

The same `/ActualText` also answers ISO 19005-4 `6.2.10.8/1`, which is the PDF/A-4
spelling of the first row.

Three defects on three independent objects, which is the masking probe: a check
that reports the first PUA it finds and stops reports one rule where three are
due. Each sits beside a conforming twin carrying the same words without the PUA
character, so a check that condemns every `/ActualText`, every `/Alt` or every
title fires on the control.

Where the PUA character goes was measured rather than assumed. `CosTextString`
is the loosest-sounding of the three objects, but a document information `/Title`
and an annotation `/Contents` were both silent; an outline item's `/Title` is
what reaches it. The controls prove the same positions, so a silent result there
means the value passed rather than the position going unread.

U+E000 is the first Private Use Area code point. It rides in a UTF-16BE string
with a byte order mark, which is how a PDF text string carries anything outside
PDFDocEncoding.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
PUA = ""


def text(value):
    """A PDF text string, UTF-16BE with a byte order mark."""
    return String(b"\xfe\xff" + value.encode("utf-16-be"))


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
            pdf,
            b"/P <</MCID 0>> BDC\n"
            b"BT /F1 12 Tf 50 750 Td (described text) Tj ET\nEMC\n"
            b"/P <</MCID 1>> BDC\n"
            b"BT /F1 12 Tf 50 720 Td (more described text) Tj ET\nEMC\n"
            b"/P <</MCID 2>> BDC\n"
            b"BT /F1 12 Tf 50 690 Td (still more) Tj ET\nEMC\n"
            b"/P <</MCID 3>> BDC\n"
            b"BT /F1 12 Tf 50 660 Td (and the last) Tj ET\nEMC\n",
        ),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

ns20 = pdf.make_indirect(Dictionary(Type=Name.Namespace, NS=String(PDF20_NS)))
struct_root = pdf.make_indirect(
    Dictionary(Type=Name.StructTreeRoot, Namespaces=Array([ns20]))
)
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Document,
        P=struct_root,
        NS=ns20,
        Lang=String("en-US"),
    )
)


def paragraph(mcid, **extra):
    """A P element over one marked-content sequence, carrying extra keys."""
    d = Dictionary(
        Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=mcid
    )
    for k, v in extra.items():
        d[Name("/" + k)] = v
    return pdf.make_indirect(d)


# 8.4.3 t2: a Private Use Area character in the text that replaces the content.
actual_bad = paragraph(0, ActualText=text("described " + PUA + " text"))
# The control: the same words, no PUA character.
actual_ok = paragraph(1, ActualText=text("described text"))

# 8.4.3 t3: the same character in the alternate description.
alt_bad = paragraph(2, Alt=text("an alternate " + PUA + " description"))
# The control.
alt_ok = paragraph(3, Alt=text("an alternate description"))

kids = [actual_bad, actual_ok, alt_bad, alt_ok]
document.K = Array(kids)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array(kids)]))
)
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# 8.6 t1: a text string a consumer reads out. An outline item's /Title reaches
# CosTextString; a document information /Title and an annotation /Contents were
# both measured silent, so the outline is what carries this one.
outline_bad = pdf.make_indirect(Dictionary(Title=text("a " + PUA + " heading")))
outline_ok = pdf.make_indirect(Dictionary(Title=text("a plain heading")))
outline_bad.Next = outline_ok
outline_ok.Prev = outline_bad
outlines = pdf.make_indirect(
    Dictionary(Type=Name.Outlines, First=outline_bad, Last=outline_ok, Count=2)
)
outline_bad.Parent = outlines
outline_ok.Parent = outlines
pdf.Root.Outlines = outlines

XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">private use text</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>2</pdfuaid:part>
   <pdfuaid:rev>2024</pdfuaid:rev>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "puatext_ua2.pdf", min_version="2.0")
