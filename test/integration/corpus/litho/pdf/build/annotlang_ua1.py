#!/usr/bin/env python3
"""Build annotlang_ua1.pdf: languages annotations name, and symbolic encodings.

Two independent constructs, neither of which any file of the seven corpora
reaches.

The /Lang of the structure element an annotation or a form field names by its
/StructParent. veraPDF reads it as a CosLang (GFPDAnnot.getLang,
GFPDFormField.getLang) whether or not the structure tree reaches the element,
and every rule scoped to CosLang judges it: ISO 14289-1, 7.2 t29, ISO 14289-2,
8.4.4 t2, ISO 19005-1, 6.8.4 t1 and ISO 19005-2 and -3, 6.7.4 t1. litho judged
only the elements its tree walk reached. Here the tree holds two paragraphs and
reaches no annotation, so these elements are the only source of those rules:

  * A1, a square whose /Annot element carries /Lang (en_US), malformed.
  * A2, a square whose element carries an empty /Lang, which the PDF/UA
    grammar fails and the PDF/A rules admit.
  * A3, the control, a square whose element carries /Lang (de-DE).
  * A4, the control, a square carrying its own malformed /Lang beside an
    element with none: veraPDF never reads an annotation's own /Lang.

A form field names its element the same way (GFPDFormField.getLang), and a
widget that is also a field is read twice. No widget is planted: the
mutate-pages and mutate-flatten cells treat widgets apart, and the unit test
TestStructParentLangValidated pins that route.

Symbolic TrueType fonts whose /Encoding dictionary names no base encoding.
ISO 14289-1, 7.21.6 t3 and its restatements (ISO 19005-1, 6.3.7 t2; ISO
19005-2 and -3, 6.2.11.6 t3; ISO 19005-4, 6.2.10.6 t3; ISO 14289-2, 8.4.5.7 t3)
forbid a symbolic TrueType font an /Encoding entry. veraPDF tests
"Encoding == null", where Encoding is the name /Encoding names or the
/BaseEncoding of the dictionary, so both fonts here pass, and litho reported
both. S1's dictionary lists /Differences only and S2's is empty but for its
/Type. Both embed the symbolic program of ttcmap_2b.pdf's T4, a single
Microsoft Symbol (3,0) subtable, and map their codes through /ToUnicode, so no
other font clause fires. No font here fails the rule, so the rule class stays
out of both tools' sets, and reporting either font adds it back.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import _truetype
from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    tounicode_cmap,
    xmp_packet,
)

pdf = pikepdf.new()

SYMBOLIC = 4  # ISO 32000-1, Table 123
program = _truetype.build([(3, 0, {0xF041: "A", 0xF042: "B"})])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
font_file = pdf.make_indirect(font_file)
to_unicode = tounicode_cmap(pdf, {0x41: 0x41, 0x42: 0x42})


def symbolic_truetype(name, encoding):
    """An embedded symbolic TrueType font drawing A and B."""
    descriptor = Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/" + name),
        Flags=SYMBOLIC,
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
        FontBBox=Array([0, -200, 550, 700]),
        FontFile2=font_file,
    )
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Font,
            Subtype=Name.TrueType,
            BaseFont=Name("/" + name),
            FirstChar=0x41,
            LastChar=0x42,
            Widths=Array([_truetype.ADVANCE_WIDTH] * 2),
            FontDescriptor=pdf.make_indirect(descriptor),
            Encoding=encoding,
            ToUnicode=to_unicode,
        )
    )


s1 = symbolic_truetype(
    "LithoSymDiff",
    Dictionary(Type=Name.Encoding, Differences=Array([0x41, Name.A, Name.B])),
)
s2 = symbolic_truetype("LithoSymEmpty", Dictionary(Type=Name.Encoding))

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(S1=s1, S2=s2)),
        Contents=Stream(
            pdf,
            b"/P <</MCID 0>> BDC BT /S1 12 Tf 50 750 Td (AB) Tj ET EMC\n"
            b"/P <</MCID 1>> BDC BT /S2 12 Tf 50 730 Td (AB) Tj ET EMC\n",
        ),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)
paragraphs = [
    pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=mcid)
    )
    for mcid in (0, 1)
]
document.K = Array(paragraphs)
struct_root.K = document

nums = [0, Array(paragraphs)]
annots = []


def appearance():
    s = Stream(pdf, b"0 0 0 RG 0 0 20 20 re S\n")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    return pdf.make_indirect(s)


def annotation(key, subtype, element_type, x, element_lang=None, **extra):
    """An annotation naming, by /StructParent key, an element the tree omits.

    The element's /P names the /Document, but the /Document's /K never lists
    it, so only the /ParentTree leads to it.
    """
    annot = Dictionary(
        Type=Name.Annot,
        Subtype=Name("/" + subtype),
        Rect=Array([x, 600, x + 20, 620]),
        F=4,
        AP=Dictionary(N=appearance()),
        StructParent=key,
    )
    for k, v in extra.items():
        annot[Name("/" + k)] = v
    annot = pdf.make_indirect(annot)
    element = Dictionary(
        Type=Name.StructElem,
        S=Name("/" + element_type),
        P=document,
        Pg=page,
        K=Dictionary(Type=Name.OBJR, Obj=annot, Pg=page),
    )
    if element_lang is not None:
        element.Lang = String(element_lang)
    nums.extend([key, pdf.make_indirect(element)])
    annots.append(annot)
    return annot


# A1 and A2: a malformed and an empty tag on elements outside the tree.
annotation(1, "Square", "Annot", 50, element_lang="en_US", Contents=String("A1"))
annotation(2, "Square", "Annot", 100, element_lang="", Contents=String("A2"))
# A3 and A4: the controls.
annotation(3, "Square", "Annot", 150, element_lang="de-DE", Contents=String("A3"))
annotation(4, "Square", "Annot", 200, Contents=String("A4"), Lang=String("x_y"))

page.Annots = Array(annots)
struct_root.ParentTree = Dictionary(Nums=Array(nums))
struct_root.ParentTreeNextKey = 5

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "annotlang_ua1.pdf", min_version="1.7")
