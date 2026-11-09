package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("annots_1b", annots1b) }

// annots1b builds annots_1b.pdf: annotation dictionary constraints under
// PDF/A-1b.
//
// The coverage scan showed PDF/A-1b as the weakest PDF/A profile in the corpus,
// with ISO 19005-1, 6.5.3 and 6.6.2 unreached between them. Both are annotation
// clauses and both are cheap: 6.5.3 constrains the annotation dictionary itself
// across six tests, and 6.6.2 forbids an additional-actions dictionary in three
// places.
//
// Nine defects sit in one file, each on its own object, which is the masking
// probe: a check that reports the first offending annotation and stops reports
// one rule where six are due, and a check that condemns every annotation once it
// sees one fires on the two conforming controls beside them.
//
// The clauses split by object, which decides where each defect has to live.
// 6.5.3 is scoped to `PDAnnot`, so its six tests are six sibling annotations on
// one page. 6.6.2 splits across `PDWidgetAnnot`, `PDFormField` and `PDDocument`,
// so its three tests need a widget, a form field and the catalog respectively.
//
// Test 3 of 6.5.3 is reachable only because the file declares no OutputIntent:
// the test permits `/C` and `/IC` when the destination profile is RGB, and with
// no output intent at all there is no RGB to permit it. The missing OutputIntent
// is itself a separate defect the file already carries.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annots1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (annotations) Tj ET\n")),
	))
	pdf.AddPage(page)

	// ISO 19005-1, 6.5.3 t2 wants /F present with Print set and Hidden, Invisible
	// and NoView clear. Bit 3 (value 4) is Print.
	const printFlag = 4

	// appearance is a minimal appearance stream, the value /N normally takes.
	appearance := func() *pdfgen.Stream {
		return pdf.Stream([]byte("0 0 0 rg 0 0 20 20 re f\n"),
			"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 20, 20))
	}

	// annot is one annotation, conforming unless extra says otherwise.
	annot := func(subtype string, y int, extra ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n(subtype),
			"Rect", a(50, y, 70, y+20),
			"F", printFlag,
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	annots := a()

	// t1: /CA other than 1.0.
	annots.Append(annot("Square", 600, "CA", 0.5, "AP", d("N", appearance())))

	// t2: no /F at all, so the Print flag cannot be set.
	aNoFlag := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(50, 570, 70, 590),
		"AP", d("N", appearance()),
	)
	annots.Append(pdf.Indirect(aNoFlag))

	// t3: a /C array with no OutputIntent to permit it.
	annots.Append(annot("Square", 540, "C", a(1, 0, 0), "AP", d("N", appearance())))

	// t4: an appearance dictionary carrying more than the /N key.
	annots.Append(annot("Square", 510, "AP", d("N", appearance(), "D", appearance())))

	// t5: a Widget whose /FT is /Btn must give /N an appearance subdictionary; this
	// one gives a stream. The field keys sit on the annotation, which is the merged
	// form the clause describes.
	annots.Append(annot("Widget", 480, "FT", n("Btn"), "T", text("btn1"), "AP", d("N", appearance())))

	// t6: a non-Widget annotation must give /N a stream; this one gives a
	// subdictionary.
	annots.Append(annot("Square", 450, "AP", d("N", d("Off", appearance()))))

	// 6.6.2 t1: a Widget annotation carrying /AA.
	widgetAA := annot("Widget", 420,
		"FT", n("Tx"),
		"T", text("txt1"),
		"AP", d("N", appearance()),
		"AA", d("K", d("S", n("JavaScript"), "JS", text("1;"))),
	)
	annots.Append(widgetAA)

	// 6.6.2 t2: a form field carrying /AA. It is a parent field rather than a
	// merged widget, so the verdict lands on PDFormField rather than on the widget.
	fieldKid := annot("Widget", 390, "T", text("kid"), "AP", d("N", appearance()))
	fieldAA := pdf.Indirect(d(
		"FT", n("Tx"),
		"T", text("parent"),
		"Kids", a(fieldKid),
		"AA", d("K", d("S", n("JavaScript"), "JS", text("1;"))),
	))
	fieldKid.Set("Parent", fieldAA)
	annots.Append(fieldKid)

	// An appearance dictionary carrying /D beside an /N of the wrong type. Both
	// /N-type tests open with the same escape, `AP != "N"`, which holds for any
	// dictionary carrying more than /N, so this annotation fails t4 alone: neither
	// t5 nor t6 judges it. litho applied that escape to t5 only and reported t6
	// here, which is why the case is planted rather than left to the two defects
	// that produce it separately.
	annots.Append(annot("Square", 300, "AP", d("N", d("Off", appearance()), "D", appearance())))

	// Two conforming annotations: a plain square and a plain widget. Both carry a
	// correct /F and an /N stream, so a firing against either is over-suppression.
	annots.Append(annot("Square", 360, "AP", d("N", appearance())))
	okWidget := annot("Widget", 330, "FT", n("Tx"), "T", text("okwidget"), "AP", d("N", appearance()))
	annots.Append(okWidget)

	page.Set("Annots", annots)

	pdf.Root.Set("AcroForm", d("Fields", a(widgetAA, fieldAA, okWidget)))
	// 6.6.2 t3: the catalog carrying /AA.
	pdf.Root.Set("AA", d("WC", d("S", n("JavaScript"), "JS", text("1;"))))

	// Deliberately no /Metadata and no /OutputIntents: the second keeps 6.5.3 t3
	// reachable, and both are independent of the annotation clauses.
	return save(pdf, "1.4")
}
