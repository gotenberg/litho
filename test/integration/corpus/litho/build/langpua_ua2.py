#!/usr/bin/env python3
"""Build langpua_ua2.pdf: Private Use Area characters in language entries.

ISO 14289-2, 8.6 t1 forbids a Private Use Area code point in a text string a
consumer reads out. veraPDF scopes it to CosTextString, whose instances are an
outline item's `/Title` (puatext_ua2.pdf covers it) and every CosLang: the
catalog `/Lang`, a structure element's `/Lang`, the `/Lang` of the element an
annotation names by `/StructParent`, and the `/Lang` of a marked-content
property list. This file carries the two a document states most often and no
other corpus file reaches:

| Construct                                  | Rule    |
| ------------------------------------------ | ------- |
| `/Lang` on a structure element             | `8.6/1` |
| `/Lang` on a BDC property list             | `8.6/1` |

Each sits beside a twin whose `/Lang` is a plain tag. A PUA character is no
letter, so both defects also break the Language-Tag grammar and fire ISO
14289-2, 8.4.4 t2, ISO 14289-1, 7.2 t29, 6.8.4 t1 at PDF/A-1a and 6.7.4 t1 at
PDF/A-2a and 3a.

The control is the annotation's own `/Lang`, which carries the same PUA
character. It is no CosLang: veraPDF reads an annotation's language from the
element its `/StructParent` names (GFPDAnnot.getLang), never from the
annotation dictionary, so neither rule may fire on it. The Annot element
enclosing it states no `/Lang`.

U+E000 is the first Private Use Area code point. It rides in a UTF-16BE string
with a byte order mark, which is how a PDF text string carries anything outside
PDFDocEncoding.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
PUA = "\ue000"
PUA_HEX = b"<FEFF0065006EE000>"


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
            # 8.6 t1: the marked-content /Lang carries the PUA character.
            b"/P <</MCID 0 /Lang " + PUA_HEX + b">> BDC\n"
            b"BT /F1 12 Tf 50 750 Td (marked text) Tj ET\nEMC\n"
            # The control: a plain tag on the same kind of property list.
            b"/P <</MCID 1 /Lang (en-GB)>> BDC\n"
            b"BT /F1 12 Tf 50 720 Td (more marked text) Tj ET\nEMC\n"
            b"/P <</MCID 2>> BDC\n"
            b"BT /F1 12 Tf 50 690 Td (element text) Tj ET\nEMC\n"
            b"/P <</MCID 3>> BDC\n"
            b"BT /F1 12 Tf 50 660 Td (more element text) Tj ET\nEMC\n",
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


bdc_bad = paragraph(0)
bdc_ok = paragraph(1)
# 8.6 t1: the structure element's /Lang carries the PUA character.
elem_bad = paragraph(2, Lang=text("en" + PUA))
# The control.
elem_ok = paragraph(3, Lang=String("en-GB"))

# The must-not-fire control: the same character in an annotation's own /Lang,
# which no CosLang reads. The Annot element enclosing it states no /Lang.
annot = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Text,
        Rect=Array([50, 600, 70, 620]),
        Contents=String("a note"),
        Lang=text("en" + PUA),
        F=4,
        StructParent=1,
    )
)
page.Annots = Array([annot])
annot_elem = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem,
        S=Name.Annot,
        P=document,
        NS=ns20,
        Pg=page,
        K=Dictionary(Type=Name.OBJR, Obj=annot, Pg=page),
    )
)

kids = [bdc_bad, bdc_ok, elem_bad, elem_ok]
document.K = Array(kids + [annot_elem])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array(kids), 1, annot_elem]))
)
struct_root.ParentTreeNextKey = 2

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">private use languages</rdf:li></rdf:Alt></dc:title>
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

save(pdf, "langpua_ua2.pdf", min_version="2.0")
