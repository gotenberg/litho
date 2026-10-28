#!/usr/bin/env python3
"""Build namedmcid_ua1.pdf: marked content naming its property list, and
Links judged per page.

A BDC operator carries its property list inline or names it from the
/Properties of the current resources (ISO 32000-1, 14.6.2), and the /MCID that
ties the content to its structure element sits in that list either way.
veraPDF resolves the name through the stream's own resources and then the
page's (PDResourcesHandler.getProperties), for a form XObject and an
annotation appearance alike. litho read the /MCID and /Lang of an inline list
alone, and resolved a form's names through the form's own resources alone. No
corpus file names a list carrying an /MCID under a structure tree.

The catalog carries no /Lang, and every structure element but two carries
one, so a language finding comes from the construct that plants it. One page
shows:

  * text under /MC0, which the page's /Properties maps to /MCID 0;
  * form /Fm1, whose text sits under /MC1 from its own /Properties;
  * form /Fm2, whose text sits under /MC2, which only the page defines;
  * text under /MC4, /MCID 2, owned by an element with no /Lang: 7.2 t34;
  * text under /MC5, /MCID 3, whose list carries the /Lang its element lacks;
  * form /Fm3, showing a glyph its /ToUnicode maps to U+E000 under /MC6, which
    only the page defines and which carries /ActualText;
  * form /Fm5, painting a square in a /Span under /MC8, which only the page
    defines and which carries /ActualText and no /Lang, inside /MC9, whose
    /MCID names an element with no /Lang: 7.2 t30;
  * three unmapped /MCID shapes veraPDF passes: /MCID 9 inside the mapped
    /MCID 1, /MCID 8 wrapping nothing, and /MCID 7 wrapping only an artifact.

Nothing is left untagged, so 7.1 test 3 and ISO 14289-2, 8.2.2 test 1 stay
silent. A second page shares the first's /Annots array, an object of its own
holding one Link written inline with no /Contents: veraPDF builds that Link
once per page, so 7.18.5 test 2 counts it twice. The XMP packet claims
PDF/UA-1, and the font is not embedded.

See ../FIXTURES.md for the authoritative expected firing set.
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
    tounicode_cmap,
    xmp_packet,
)

pdf = pikepdf.new()

LANG = String("en-US")

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)
# A code the /ToUnicode map sends into the Private Use Area (ISO 14289-2,
# 8.4.3), which only an /ActualText covers.
pua_font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
        ToUnicode=tounicode_cmap(pdf, {0x78: 0xE000}),
    )
)


def text(y, s, face=b"F1"):
    return b"BT /%s 12 Tf 50 %d Td (%s) Tj ET\n" % (face, y, s)


def form(content, resources, struct_parents):
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 612, 792])
    s.Resources = resources
    s.StructParents = struct_parents
    return pdf.make_indirect(s)


fonts = Dictionary(F1=font, F2=pua_font)
fm1 = form(
    b"/P /MC1 BDC\n" + text(700, b"a form naming its own list") + b"EMC\n",
    Dictionary(Font=fonts, Properties=Dictionary(MC1=Dictionary(MCID=0))),
    1,
)
fm2 = form(b"/P /MC2 BDC\n" + text(680, b"a form naming the page list") + b"EMC\n", Dictionary(Font=fonts), 2)
fm3 = form(b"/Span /MC6 BDC\n" + text(660, b"x", b"F2") + b"EMC\n", Dictionary(Font=fonts), 3)
fm5 = form(b"/P /MC9 BDC\n/Span /MC8 BDC\n50 600 10 10 re f\nEMC\nEMC\n", Dictionary(Font=fonts), 5)

content = (
    b"/P /MC0 BDC\n"
    + text(740, b"the page naming its list")
    + b"EMC\n"
    b"q /Fm1 Do Q\nq /Fm2 Do Q\nq /Fm3 Do Q\nq /Fm5 Do Q\n"
    b"/P /MC4 BDC\n" + text(560, b"owned by an element with no language") + b"EMC\n"
    b"/P /MC5 BDC\n" + text(540, b"its list names the language") + b"EMC\n"
    b"/P <</MCID 1>> BDC\n/Span <</MCID 9>> BDC\n" + text(520, b"inside a mapped sequence") + b"EMC\nEMC\n"
    b"/P <</MCID 8>> BDC\nEMC\n"
    b"/P <</MCID 7>> BDC\n/Artifact <</Lang (en-US)>> BDC\n" + text(500, b"an artifact") + b"EMC\nEMC\n"
)

properties = Dictionary(
    MC0=Dictionary(MCID=0),
    MC2=Dictionary(MCID=0),
    MC4=Dictionary(MCID=2),
    MC5=Dictionary(MCID=3, Lang=LANG),
    MC6=Dictionary(MCID=0, ActualText=String("private")),
    MC8=Dictionary(ActualText=String("a square")),
    MC9=Dictionary(MCID=0),
)

link = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Link,
    Rect=Array([50, 400, 200, 420]),
    Border=Array([0, 0, 0]),
    A=Dictionary(S=Name.URI, URI=String("https://example.com/")),
)
annots = pdf.make_indirect(Array([link]))

page1 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=fonts,
            XObject=Dictionary(Fm1=fm1, Fm2=fm2, Fm3=fm3, Fm5=fm5),
            Properties=properties,
        ),
        Contents=Stream(pdf, content),
        StructParents=0,
        Tabs=Name.S,
        Annots=annots,
    )
)
pdf.pages.append(pikepdf.Page(page1))
page2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Tabs=Name.S,
        Annots=annots,
    )
)
pdf.pages.append(pikepdf.Page(page2))
page1 = pdf.pages[0].obj

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))


def elem(k, lang=True):
    d = Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page1, K=k)
    if lang:
        d.Lang = LANG
    return pdf.make_indirect(d)


def mcr(stm):
    return Dictionary(Type=Name.MCR, MCID=0, Stm=stm)


page_p0 = elem(0)
page_p1 = elem(1)
nolang = elem(2, lang=False)
listlang = elem(3, lang=False)
form_p1 = elem(mcr(fm1))
form_p2 = elem(mcr(fm2))
form_p3 = elem(mcr(fm3))
form_p5 = elem(mcr(fm5), lang=False)
document.K = Array([page_p0, page_p1, nolang, listlang, form_p1, form_p2, form_p3, form_p5])
struct_root.K = Array([document])
# The page maps MCIDs 0 to 3 and leaves 7, 8 and 9 unmapped; each form's
# /StructParents keys its own entry, whose MCID 0 names its element.
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [
                0,
                Array([page_p0, page_p1, nolang, listlang]),
                1,
                Array([form_p1]),
                2,
                Array([form_p2]),
                3,
                Array([form_p3]),
                5,
                Array([form_p5]),
            ]
        )
    )
)
struct_root.ParentTreeNextKey = 6

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

save(pdf, "namedmcid_ua1.pdf", min_version="1.7")
