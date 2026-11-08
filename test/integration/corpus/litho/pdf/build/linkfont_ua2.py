#!/usr/bin/env python3
"""Build linkfont_ua2.pdf: link targets and a symbolic font's character map.

Two ISO 14289-2 rules the coverage scan showed unreached, on unrelated parts of
the document:

| Construct                                              | Rule           |
| ------------------------------------------------------ | -------------- |
| two links under one `Link` element onto different targets | `8.2.5.20/2` |
| a symbolic TrueType with neither a (3,0) nor a (1,0) `cmap` | `8.4.5.7/4` |

`8.2.5.20/2` is the one worth the file. veraPDF only compares link targets that
are STRUCTURE destinations, and it compares them by object identity alone: a
destination array's first element must be a dictionary carrying `/S`, and the
view parameters that follow it are ignored. A page destination, a URI action, or
a destination naming no structure element leaves nothing to compare and the rule
is never evaluated at all.

So the file carries three groups, and only the first offends:

  bad   two links under one `Link` element, with `/SD` naming two DIFFERENT
        structure elements. Fires.
  same  two links under one `Link` element, both naming the SAME structure
        element but with different view parameters. Silent, and it is the
        control that matters most: comparing whole destination arrays rather
        than target identity reports this pair as disagreeing.
  page  two links under one `Link` element with ordinary page destinations,
        differing from each other. Silent, because a page destination is not a
        structure destination and never reaches the comparison.

`8.4.5.7/4` reads `isSymbolic == false || cmap30Present == true || cmap10Present
== true`, so it needs a font that is symbolic AND carries neither subtable. The
program here holds a (3,1) subtable only. Beside it sits the same program
declared non-symbolic, which the first branch excuses, so the discriminator is
the flag rather than the character map.

That symbolic font is painted in rendering mode 3, and the reason is a divergence
rather than a preference. A symbolic TrueType with neither subtable cannot map
its codes at all, so veraPDF reports every code it paints as a glyph that is not
present (`8.4.5.5.1/2`); litho leaves a code it cannot map unchecked, on the
ground that presence is then unresolvable, and reports nothing. Mode 3 exempts
the glyphs on both sides, which keeps this file about the character map while
that question stays open. It is a real litho false negative, recorded rather
than planted: reaching it needs the font painted visibly, and the fix means
turning an unresolvable code into a firing one, which is the direction that has
produced false positives on real-world corpora before and wants a wide sweep
first.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

PDF20_NS = "http://iso.org/pdf2/ssn"
SYMBOLIC = 4
NONSYMBOLIC = 32
LATIN = {0x41: "A", 0x42: "B"}

helvetica = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def truetype(name, flags, encoding=None):
    """A simple TrueType whose program carries a (3,1) subtable and no other.

    A non-symbolic font needs an /Encoding for its codes to have a defined
    meaning; a symbolic one must not declare one, so the caller chooses.
    """
    program = _truetype.build([(3, 1, LATIN)])
    font_file = Stream(pdf, program)
    font_file.Length1 = len(program)
    descriptor = pdf.make_indirect(
        Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name("/" + name),
            Flags=flags,
            ItalicAngle=0,
            Ascent=800,
            Descent=-200,
            CapHeight=700,
            StemV=80,
            FontBBox=Array([0, -200, 550, 700]),
            FontFile2=pdf.make_indirect(font_file),
        )
    )
    font = Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/" + name),
        FirstChar=0x41,
        LastChar=0x42,
        Widths=Array([_truetype.ADVANCE_WIDTH, _truetype.ADVANCE_WIDTH]),
        FontDescriptor=descriptor,
    )
    if encoding is not None:
        font.Encoding = Name("/" + encoding)
    return pdf.make_indirect(font)


# 8.4.5.7 t4: symbolic, and the program carries neither (3,0) nor (1,0). The
# non-symbolic twin beside it is excused by the test's first branch.
font_symbolic = truetype("LithoSymNoCmap", SYMBOLIC)
font_plain = truetype("LithoPlainCmap", NONSYMBOLIC, "WinAnsiEncoding")

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=helvetica, TS=font_symbolic, TP=font_plain)
        ),
        Contents=Stream(
            pdf,
            b"/P <</MCID 0>> BDC\n"
            b"BT /F1 12 Tf 50 750 Td (link targets and character maps) Tj ET\n"
            b"BT 3 Tr /TS 12 Tf 50 720 Td (AB) Tj ET\n"
            b"BT /TP 12 Tf 50 690 Td (AB) Tj ET\nEMC\n",
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
paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, NS=ns20, Pg=page, K=0)
)

# The two structure elements a structure destination can name. They hold no
# content of their own; only their identity matters to the comparison.
target_a = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Sect, P=document, NS=ns20, Pg=page)
)
target_b = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Sect, P=document, NS=ns20, Pg=page)
)

annots = []
doc_kids = [paragraph, target_a, target_b]
nums = [0, Array([paragraph])]
next_key = 1


def appearance():
    ap = Stream(pdf, b"0 0 1 RG 0 0 40 20 re S\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 40, 20])
    return pdf.make_indirect(ap)


def link(y, dest, structured):
    """A Link annotation whose GoTo action names dest.

    A structure destination rides on /SD, which ISO 32000-2 12.6.4.2 gives
    precedence over /D; an ordinary page destination rides on /D.
    """
    action = Dictionary(S=Name.GoTo)
    if structured:
        action.SD = dest
    else:
        action.D = dest
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Link,
            Rect=Array([50, y, 90, y + 20]),
            F=4,
            Contents=String("a link"),
            AP=Dictionary(N=appearance()),
            A=action,
        )
    )


def link_group(links):
    """Wire a set of Link annotations under one shared Link element."""
    global next_key
    element = pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Link,
            P=document,
            NS=ns20,
            Pg=page,
            K=Array([Dictionary(Type=Name.OBJR, Obj=lk) for lk in links]),
        )
    )
    for lk in links:
        lk.StructParent = next_key
        nums.extend([next_key, element])
        next_key += 1
        annots.append(lk)
    doc_kids.append(element)
    return element


# The defect: two structure destinations naming different elements.
link_group(
    [
        link(650, Array([target_a, Name.Fit]), structured=True),
        link(620, Array([target_b, Name.Fit]), structured=True),
    ]
)

# The control that matters: the same element, different view parameters. A
# comparison that reads the whole destination array reports these as disagreeing.
link_group(
    [
        link(590, Array([target_a, Name.Fit]), structured=True),
        link(560, Array([target_a, Name.XYZ, 0, 700, 0]), structured=True),
    ]
)

# The second control: ordinary page destinations, differing from each other.
# They are not structure destinations, so they never reach the comparison.
link_group(
    [
        link(530, Array([page, Name.Fit]), structured=False),
        link(500, Array([page, Name.XYZ, 0, 700, 0]), structured=False),
    ]
)

page.Annots = Array(annots)

document.K = Array(doc_kids)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array(nums)))
struct_root.ParentTreeNextKey = next_key

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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">link targets and character maps</rdf:li></rdf:Alt></dc:title>
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

save(pdf, "linkfont_ua2.pdf", min_version="2.0")
