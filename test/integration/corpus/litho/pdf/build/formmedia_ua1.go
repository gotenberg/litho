package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formmedia_ua1", formmediaUa1) }

// formmediaUa1 builds formmedia_ua1.pdf: formulas, notes, shared XObjects and media clips.
//
// The coverage scan showed five ISO 14289-1 rules unreached, each scoped to a
// different veraPDF object, so a file reaches each only by carrying that exact
// construct:
//
// | Construct                                     | Rule          |
// | --------------------------------------------- | ------------- |
// | a `Formula` with neither `/Alt` nor `/ActualText` | `7.7/1`   |
// | two `Note` elements sharing an `/ID`          | `7.9/2`       |
// | a Form XObject painted under two elements     | `7.20/2`      |
// | a media clip with no `/CT`                    | `7.18.6.2/1`  |
// | a media clip whose `/Alt` carries no text     | `7.18.6.2/2`  |
//
// Five independent axes in one file, which is the masking probe: a check that
// abandons a document after the first accessibility complaint reports one rule
// where five are due.
//
// The two media clip tests are deliberately on separate clips. A single clip
// missing both `/CT` and a usable `/Alt` would report one rule and leave the other
// unproven, because a check that gives up on the first problem never reaches the
// second. So one clip carries a valid `/Alt` and no `/CT`, the other carries `/CT`
// and an `/Alt` whose text is empty, and a third carries both and is the control.
//
// `7.20/2` turns on something the clause text does not say and measurement did:
// the check exits early for a Form XObject carrying no `/StructParents`, so such a
// form can be painted any number of times and answer nothing. Reaching the rule
// needs the key AND the same indirect object encountered twice. Painting a form
// twice under two `P` elements without the key was measured silent, as were the
// same construct across two pages and one whose own content is marked. So the
// shared form here carries `/StructParents`, and the control beside it carries the
// key too and is painted once: the discriminator is the repetition, not the key.
//
// Controls sit beside every defect: a `Formula` carrying `/Alt`, a third `Note`
// with an `/ID` of its own, the singly-painted Form XObject, and the complete
// media clip. veraPDF reports a rule once however many objects break it, so
// rule-set agreement alone cannot see them; the per-rule failed-check counts are
// what prove them silent.
//
// The font is not embedded, so the font clauses fire independently of everything
// above and a structure verdict stays visibly separate from them.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formmediaUa1() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// square is a Form XObject painting one small square, inside its own
	// marked content.
	//
	// /StructParents is what brings the form into veraPDF's scope for 7.20 t2: the
	// check returns early for a form that carries none, so a form without it can
	// be painted any number of times and answer nothing.
	square := func(colour string, structParents int) *pdfgen.Stream {
		stream := pdf.Stream(
			fmt.Appendf(nil, "/P <</MCID 0>> BDC\n%s 0 0 20 20 re f\nEMC\n", colour),
		)
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 20, 20))
		stream.Set("Resources", d())
		stream.Set("StructParents", structParents)
		pdf.MakeIndirect(stream)
		return stream
	}

	formShared := square("1 0 0 rg", 1)
	formSolo := square("0 0 1 rg", 2)

	// Six marked-content sequences. MCID 0 and 1 both paint /FmS, and they belong to
	// different structure elements, which is what gives that XObject two semantic
	// parents. Everything else is painted once.
	content := "/P <</MCID 0>> BDC\nq 1 0 0 1 60 700 cm /FmS Do Q\nEMC\n" +
		"/P <</MCID 1>> BDC\nq 1 0 0 1 100 700 cm /FmS Do Q\nEMC\n" +
		"/P <</MCID 2>> BDC\nq 1 0 0 1 140 700 cm /FmO Do Q\nEMC\n" +
		"/Formula <</MCID 3>> BDC\nBT /F1 12 Tf 50 650 Td (E = mc2) Tj ET\nEMC\n" +
		"/Formula <</MCID 4>> BDC\nBT /F1 12 Tf 50 620 Td (a2 + b2 = c2) Tj ET\nEMC\n" +
		"/Note <</MCID 5>> BDC\nBT /F1 12 Tf 50 590 Td (first note) Tj ET\nEMC\n" +
		"/Note <</MCID 6>> BDC\nBT /F1 12 Tf 50 560 Td (second note) Tj ET\nEMC\n" +
		"/Note <</MCID 7>> BDC\nBT /F1 12 Tf 50 530 Td (third note) Tj ET\nEMC\n"

	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 0 rg 0 0 40 20 re f\n"))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 40, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	// mediaScreen is a Screen annotation whose rendition action carries the
	// given clip.
	mediaScreen := func(y int, clip *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Screen"),
			"Rect", a(50, y, 90, y+20),
			"F", 4,
			"Contents", text("a media screen"),
			"AP", d("N", appearance()),
			"A", d(
				"S", n("Rendition"),
				"OP", 0,
				"R", d("Type", n("Rendition"), "S", n("MR"), "C", clip),
			),
		))
	}

	// clip is a media clip dictionary. /CT and /Alt are supplied by the
	// caller.
	clip := func(name, ct string, alt *pdfgen.Array) *pdfgen.Dict {
		dd := d("Type", n("MediaClip"), "S", n("MCD"), "N", text(name))
		if ct != "" {
			dd.Set("CT", text(ct))
		}
		if alt != nil {
			dd.Set("Alt", alt)
		}
		return pdf.Indirect(dd)
	}

	// 7.18.6.2 t1: no /CT, but a usable /Alt so the other test stays unanswered here.
	clipNoCT := clip("no content type", "", a(text("en-US"), text("a described clip")))
	// 7.18.6.2 t2: a /CT, and an /Alt whose only text is empty.
	clipBadAlt := clip("no alternate text", "audio/mpeg", a(text("en-US"), text("")))
	// The control: both present and usable.
	clipOK := clip("complete", "audio/mpeg", a(text("en-US"), text("a described clip")))

	annots := a(
		mediaScreen(480, clipNoCT),
		mediaScreen(450, clipBadAlt),
		mediaScreen(420, clipOK),
	)

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"XObject", d("FmS", formShared, "FmO", formSolo),
		),
		"Contents", pdf.Stream([]byte(content)),
		"Annots", annots,
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(
		d("Type", n("StructElem"), "S", n("Document"), "P", structRoot, "Lang", text("en-US")),
	)

	element := func(kind string, mcid int, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n(kind), "P", document, "Pg", page, "K", mcid)
		for i := 0; i < len(extra); i += 2 {
			k := extra[i].(string)
			if extra[i+1] == nil {
				dd.Del(k)
				continue
			}
			dd.Set(k, extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	// 7.20 t2: MCID 0 and 1 both paint /FmS, under two different elements. The rule
	// turns on the form being reached twice, not on which elements reach it, so the
	// singly-painted /FmO beside it carries /StructParents too: that is what proves
	// the discriminator is the repetition rather than the key.
	sharedA := element("P", 0)
	sharedB := element("P", 1)
	solo := element("P", 2)

	// The elements that own each form's own marked content, which /StructParents
	// points at.
	formSharedContent := element("P", 0, "Pg", nil)
	formSoloContent := element("P", 0, "Pg", nil)

	// 7.7 t1: a Formula describing itself neither way, beside one that carries /Alt.
	formulaBad := element("Formula", 3)
	formulaOK := element("Formula", 4, "Alt", text("a squared plus b squared equals c squared"))

	// 7.9 t2: two Notes sharing an /ID, beside a third whose /ID is its own.
	noteA := element("Note", 5, "ID", text("shared-note-id"))
	noteB := element("Note", 6, "ID", text("shared-note-id"))
	noteC := element("Note", 7, "ID", text("unique-note-id"))

	kids := []any{
		sharedA, sharedB, solo, formulaBad, formulaOK, noteA, noteB, noteC,
		formSharedContent, formSoloContent,
	}
	document.Set("K", a(kids...))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(0, a(kids...), 1, a(formSharedContent), 2, a(formSoloContent)),
	)))
	structRoot.Set("ParentTreeNextKey", 3)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
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
<?xpacket end="w"?>`
	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7")
}
