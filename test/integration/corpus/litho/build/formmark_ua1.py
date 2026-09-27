#!/usr/bin/env python3
"""Build formmark_ua1.pdf: form XObjects judged by what they draw.

ISO 14289-1, 7.1 asks that real content be tagged and everything else be
marked as an artifact (test 3), and that neither sit inside the other (tests 1
and 2). veraPDF builds its content items from what a form XObject draws, in the
marked-content state around the Do (ISO 32000-1, 14.6), so a form that paints
nothing is no content and the sequences a form opens are judged as the page's
are. litho counted every Do of a form as one untagged content item and read no
marked content inside a form, and no corpus file drew a form that paints
nothing inside a tagged document, or opened a sequence inside a form.

One tagged page paints a square as a /P and draws six forms:

  * /Empty, a form with an empty content stream, outside marked content.
  * /ArtForm, painting a square inside its own /Artifact sequence.
  * /TagForm, with /StructParents 1, painting a square inside a /P sequence
    whose /MCID 3 its ParentTree entry maps to a /P element. The page's entry
    maps MCIDs 0 and 1 only, so the MCID resolves through the form's
    /StructParents or not at all.
  * /ObjrForm, with /StructParent 2, painting a square outside any marked
    content: the /Figure its ParentTree entry names, through an OBJR, tags
    everything it draws.
  * /Chain, drawing /Inner, whose content stream is empty.
  * /NestForm, drawn inside the page's /P sequence with /MCID 1. It has no
    /StructParents, so its own /MCID 0 resolves nowhere and its square takes
    the page's sequence as its tag. It then paints a second square inside an
    /Artifact sequence, which opens inside tagged content: 7.1 tests 1 and 2.

No form paints untagged content, so 7.1 test 3 and ISO 14289-2, 8.2.2 test 1
stay silent, and the two findings come from inside a form. The page shows no
text, so no font clause fires. The XMP packet claims PDF/UA-1.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

SQUARE = b"0 0 10 10 re f\n"


def form(content, **extra):
    """A form XObject over a 20 by 20 box, with its own /Resources."""
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    if "Resources" not in extra:
        s.Resources = Dictionary()
    for key, value in extra.items():
        s[Name("/" + key)] = value
    return pdf.make_indirect(s)


empty = form(b"")
art_form = form(b"/Artifact BMC\n" + SQUARE + b"EMC\n")
tag_form = form(b"/P <</MCID 3>> BDC\n" + SQUARE + b"EMC\n", StructParents=1)
objr_form = form(SQUARE, StructParent=2)
inner = form(b"")
chain = form(b"/Inner Do\n", Resources=Dictionary(XObject=Dictionary(Inner=inner)))
nest_form = form(
    b"/Span <</MCID 0>> BDC\n" + SQUARE + b"EMC\n"
    b"/Artifact BMC\n0 10 10 10 re f\nEMC\n"
)

content = (
    b"/P <</MCID 0>> BDC\n50 700 100 20 re f\nEMC\n"
    b"q 1 0 0 1 50 660 cm /Empty Do Q\n"
    b"q 1 0 0 1 50 620 cm /ArtForm Do Q\n"
    b"q 1 0 0 1 50 580 cm /TagForm Do Q\n"
    b"q 1 0 0 1 50 540 cm /ObjrForm Do Q\n"
    b"q 1 0 0 1 50 500 cm /Chain Do Q\n"
    b"/P <</MCID 1>> BDC\nq 1 0 0 1 50 460 cm /NestForm Do Q\nEMC\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            XObject=Dictionary(
                Empty=empty,
                ArtForm=art_form,
                TagForm=tag_form,
                ObjrForm=objr_form,
                Chain=chain,
                NestForm=nest_form,
            )
        ),
        Contents=pdf.make_indirect(Stream(pdf, content)),
        StructParents=0,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root)
)


def elem(tag, k, **extra):
    return pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=tag, P=document, Pg=page, K=k, **extra)
    )


p0 = elem(Name.P, 0)
p1 = elem(Name.P, 1)
p_form = elem(Name.P, Dictionary(Type=Name.MCR, MCID=3, Stm=tag_form))
figure = elem(
    Name.Figure,
    Dictionary(Type=Name.OBJR, Obj=objr_form),
    Alt=String("a square"),
)
document.K = Array([p0, p1, p_form, figure])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([p0, p1]),
                1,
                Array([None, None, None, p_form]),
                2,
                figure,
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 3

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "formmark_ua1.pdf", min_version="1.7")
