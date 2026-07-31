#!/usr/bin/env python3
"""Build formmedia_ua1.pdf: formulas, notes, shared XObjects and media clips.

The coverage scan showed five ISO 14289-1 rules unreached, each scoped to a
different veraPDF object, so a file reaches each only by carrying that exact
construct:

| Construct                                     | Rule          |
| --------------------------------------------- | ------------- |
| a `Formula` with neither `/Alt` nor `/ActualText` | `7.7/1`   |
| two `Note` elements sharing an `/ID`          | `7.9/2`       |
| a Form XObject painted under two elements     | `7.20/2`      |
| a media clip with no `/CT`                    | `7.18.6.2/1`  |
| a media clip whose `/Alt` carries no text     | `7.18.6.2/2`  |

Five independent axes in one file, which is the masking probe: a check that
abandons a document after the first accessibility complaint reports one rule
where five are due.

The two media clip tests are deliberately on separate clips. A single clip
missing both `/CT` and a usable `/Alt` would report one rule and leave the other
unproven, because a check that gives up on the first problem never reaches the
second. So one clip carries a valid `/Alt` and no `/CT`, the other carries `/CT`
and an `/Alt` whose text is empty, and a third carries both and is the control.

`7.20/2` turns on something the clause text does not say and measurement did:
the check exits early for a Form XObject carrying no `/StructParents`, so such a
form can be painted any number of times and answer nothing. Reaching the rule
needs the key AND the same indirect object encountered twice. Painting a form
twice under two `P` elements without the key was measured silent, as were the
same construct across two pages and one whose own content is marked. So the
shared form here carries `/StructParents`, and the control beside it carries the
key too and is painted once: the discriminator is the repetition, not the key.

Controls sit beside every defect: a `Formula` carrying `/Alt`, a third `Note`
with an `/ID` of its own, the singly-painted Form XObject, and the complete
media clip. veraPDF reports a rule once however many objects break it, so
rule-set agreement alone cannot see them; the per-rule failed-check counts are
what prove them silent.

The font is not embedded, so the font clauses fire independently of everything
above and a structure verdict stays visibly separate from them.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)


def square(colour, struct_parents):
    """A Form XObject painting one small square, inside its own marked content.

    /StructParents is what brings the form into veraPDF's scope for 7.20 t2: the
    check returns early for a form that carries none, so a form without it can
    be painted any number of times and answer nothing.
    """
    stream = Stream(
        pdf,
        ("/P <</MCID 0>> BDC\n%s 0 0 20 20 re f\nEMC\n" % colour).encode("ascii"),
    )
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 20, 20])
    stream.Resources = Dictionary()
    stream.StructParents = struct_parents
    return pdf.make_indirect(stream)


form_shared = square("1 0 0 rg", 1)
form_solo = square("0 0 1 rg", 2)

# Six marked-content sequences. MCID 0 and 1 both paint /FmS, and they belong to
# different structure elements, which is what gives that XObject two semantic
# parents. Everything else is painted once.
CONTENT = (
    b"/P <</MCID 0>> BDC\nq 1 0 0 1 60 700 cm /FmS Do Q\nEMC\n"
    b"/P <</MCID 1>> BDC\nq 1 0 0 1 100 700 cm /FmS Do Q\nEMC\n"
    b"/P <</MCID 2>> BDC\nq 1 0 0 1 140 700 cm /FmO Do Q\nEMC\n"
    b"/Formula <</MCID 3>> BDC\nBT /F1 12 Tf 50 650 Td (E = mc2) Tj ET\nEMC\n"
    b"/Formula <</MCID 4>> BDC\nBT /F1 12 Tf 50 620 Td (a2 + b2 = c2) Tj ET\nEMC\n"
    b"/Note <</MCID 5>> BDC\nBT /F1 12 Tf 50 590 Td (first note) Tj ET\nEMC\n"
    b"/Note <</MCID 6>> BDC\nBT /F1 12 Tf 50 560 Td (second note) Tj ET\nEMC\n"
    b"/Note <</MCID 7>> BDC\nBT /F1 12 Tf 50 530 Td (third note) Tj ET\nEMC\n"
)


def appearance():
    ap = Stream(pdf, b"0 0 0 rg 0 0 40 20 re f\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 40, 20])
    return pdf.make_indirect(ap)


def media_screen(y, clip):
    """A Screen annotation whose rendition action carries the given clip."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Annot,
            Subtype=Name.Screen,
            Rect=Array([50, y, 90, y + 20]),
            F=4,
            Contents=String("a media screen"),
            AP=Dictionary(N=appearance()),
            A=Dictionary(
                S=Name.Rendition,
                OP=0,
                R=Dictionary(Type=Name.Rendition, S=Name.MR, C=clip),
            ),
        )
    )


def clip(name, ct=None, alt=None):
    """A media clip dictionary. /CT and /Alt are supplied by the caller."""
    d = Dictionary(Type=Name.MediaClip, S=Name.MCD, N=String(name))
    if ct is not None:
        d.CT = String(ct)
    if alt is not None:
        d.Alt = alt
    return pdf.make_indirect(d)


# 7.18.6.2 t1: no /CT, but a usable /Alt so the other test stays unanswered here.
clip_no_ct = clip("no content type", alt=Array([String("en-US"), String("a described clip")]))
# 7.18.6.2 t2: a /CT, and an /Alt whose only text is empty.
clip_bad_alt = clip("no alternate text", ct="audio/mpeg", alt=Array([String("en-US"), String("")]))
# The control: both present and usable.
clip_ok = clip("complete", ct="audio/mpeg", alt=Array([String("en-US"), String("a described clip")]))

annots = [
    media_screen(480, clip_no_ct),
    media_screen(450, clip_bad_alt),
    media_screen(420, clip_ok),
]

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            XObject=Dictionary(FmS=form_shared, FmO=form_solo),
        ),
        Contents=Stream(pdf, CONTENT),
        Annots=Array(annots),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US"))
)


def element(kind, mcid, **extra):
    d = Dictionary(Type=Name.StructElem, S=Name("/" + kind), P=document, Pg=page, K=mcid)
    for k, v in extra.items():
        if v is None:
            del d[Name("/" + k)]
            continue
        d[Name("/" + k)] = v
    return pdf.make_indirect(d)


# 7.20 t2: MCID 0 and 1 both paint /FmS, under two different elements. The rule
# turns on the form being reached twice, not on which elements reach it, so the
# singly-painted /FmO beside it carries /StructParents too: that is what proves
# the discriminator is the repetition rather than the key.
shared_a = element("P", 0)
shared_b = element("P", 1)
solo = element("P", 2)

# The elements that own each form's own marked content, which /StructParents
# points at.
form_shared_content = element("P", 0, Pg=None)
form_solo_content = element("P", 0, Pg=None)

# 7.7 t1: a Formula describing itself neither way, beside one that carries /Alt.
formula_bad = element("Formula", 3)
formula_ok = element("Formula", 4, Alt=String("a squared plus b squared equals c squared"))

# 7.9 t2: two Notes sharing an /ID, beside a third whose /ID is its own.
note_a = element("Note", 5, ID=String("shared-note-id"))
note_b = element("Note", 6, ID=String("shared-note-id"))
note_c = element("Note", 7, ID=String("unique-note-id"))

kids = [shared_a, shared_b, solo, formula_bad, formula_ok, note_a, note_b, note_c,
        form_shared_content, form_solo_content]
document.K = Array(kids)
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(
        Nums=Array(
            [0, Array(kids), 1, Array([form_shared_content]), 2, Array([form_solo_content])]
        )
    )
)
struct_root.ParentTreeNextKey = 3

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
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">formulas, notes and media</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>1</pdfuaid:part>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "formmedia_ua1.pdf", min_version="1.7")
