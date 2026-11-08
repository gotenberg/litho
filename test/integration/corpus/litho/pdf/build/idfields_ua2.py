#!/usr/bin/env python3
"""Build idfields_ua2.pdf: identification prefixes, destinations and text fields.

Four ISO 14289-2 rules the coverage scan showed unreached, on four unrelated
parts of the document, which is what makes the file a masking probe rather than
four separate documents:

| Construct                                          | Rule          |
| -------------------------------------------------- | ------------- |
| `part` bound to a prefix other than `pdfuaid`      | `5/3`         |
| `rev` bound to a prefix other than `pdfuaid`       | `5/4`         |
| an outline destination naming a page               | `8.8/1`       |
| a text field whose `/RV` disagrees with its `/V`   | `8.10.3.3/1`  |

The two identification defects are the load-bearing pair. XMP identifies a
schema by NAMESPACE, not by prefix, so declaring a second prefix on the PDF/UA
identification namespace and hanging `part` and `rev` off it keeps the file
identifiable as PDF/UA-2 while making both prefixes wrong. If it stopped being
identifiable the other rules would go unevaluated and the file would report
almost nothing, so those two rules firing alongside the rest is itself the proof
the trick worked.

`8.8` splits by object: test 1 judges a `PDDestination` and test 2 a
`PDGoToAction`. Only test 1 is planted here, on an outline item's `/Dest`;
`annotstruct_ua2.pdf` already covers test 2 through link annotations. Beside it
sits an outline item whose `/Dest` does name a structure element, which is the
control.

`8.10.3.3/1` reads `containsRV == false || (V != null && RV == V)`, so a field
carrying rich text must carry the plain equivalent and the two must agree. The
control carries both, equal.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def appearance(w=120, h=20):
    ap = Stream(pdf, b"0.9 0.9 0.9 rg 0 0 %d %d re f\n" % (w, h))
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, w, h])
    return pdf.make_indirect(ap)


# 8.10.3.3 t1: /RV present and disagreeing with /V, beside one where they agree.
RV_BAD = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>something else</p></body>"
RV_OK = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>agreed text</p></body>"

field_bad = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 600, 170, 620]),
        F=4,
        FT=Name.Tx,
        T=String("disagreeing"),
        TU=String("a field whose rich text disagrees"),
        V=String("the plain text"),
        RV=String(RV_BAD),
        AP=Dictionary(N=appearance()),
        StructParent=1,
    )
)
field_ok = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 560, 170, 580]),
        F=4,
        FT=Name.Tx,
        T=String("agreeing"),
        TU=String("a field whose rich text agrees"),
        V=String("agreed text"),
        RV=String(RV_OK),
        AP=Dictionary(N=appearance()),
        StructParent=2,
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
            b"BT /F1 12 Tf 50 750 Td (identification and destinations) Tj ET\nEMC\n",
        ),
        Annots=Array([field_bad, field_ok]),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AcroForm = Dictionary(
    Fields=Array([field_bad, field_ok]),
    DA=String("/Helv 0 Tf 0 g"),
    DR=Dictionary(Font=Dictionary(Helv=font)),
)

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
paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)


def form_element(widget):
    """A Form element enclosing one widget, which ISO 14289-2 8.10.1 requires."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Form,
            P=document,
            NS=ns20,
            Pg=page,
            K=Array([Dictionary(Type=Name.OBJR, Obj=widget)]),
        )
    )


form_bad = form_element(field_bad)
form_ok = form_element(field_ok)

document.K = Array([paragraph, form_bad, form_ok])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([paragraph]), 1, form_bad, 2, form_ok]))
)
struct_root.ParentTreeNextKey = 3

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# 8.8 t1: an outline destination naming a page rather than a structure element,
# beside one that names the Document element and is the control.
outline_bad = pdf.make_indirect(
    Dictionary(Title=String("a page destination"), Dest=Array([page, Name.Fit]))
)
outline_ok = pdf.make_indirect(
    Dictionary(
        Title=String("a structure destination"), Dest=Array([document, Name.Fit])
    )
)
outline_bad.Next = outline_ok
outline_ok.Prev = outline_bad
outlines = pdf.make_indirect(
    Dictionary(Type=Name.Outlines, First=outline_bad, Last=outline_ok, Count=2)
)
outline_bad.Parent = outlines
outline_ok.Parent = outlines
pdf.Root.Outlines = outlines

# 5 t3 and t4: a second prefix declared on the PDF/UA identification namespace,
# with part and rev hanging off it. The namespace is what identifies the schema,
# so the file stays a PDF/UA-2 file while both prefixes are wrong.
XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
    xmlns:ua="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">identification prefixes</rdf:li></rdf:Alt></dc:title>
   <ua:part>2</ua:part>
   <ua:rev>2024</ua:rev>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "idfields_ua2.pdf", min_version="2.0")
