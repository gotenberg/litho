package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("fartext_1b", fartext1b) }

// fartext1b builds fartext_1b.pdf: content placed by numbers past the PDF/A-1
// real limit of 32767 (ISO 19005-1, 6.1.12 t2), where clamping the number
// would move or reshape what the page shows, beside numbers whose clamp
// changes nothing a reader sees.
//
// Producers that lay a long flow out once and show each page's slice of it
// under a translation write such numbers: a translation of -40000 brings
// back text whose matrix places it 40100 points down. Clamping that matrix
// to 32767 puts the text 7000 points below the page, where no viewer shows
// it and text extraction no longer reads it.
//
//   - Page 1: its text sits under the text matrix 1 0 0 1 20 40100.5, behind
//     an integer translation PDF/A-1 admits (t1 bounds integers at 2^31 - 1).
//     A remediation preserving content leaves the matrix, and the violation
//     stays a residual.
//   - Page 2: the control. Its text sits under the integer matrix
//     1 0 0 1 20 40100 behind a real translation of -40000.5, which a
//     remediation writes as two translations inside the limit, drawing the
//     page as written.
//   - Page 3: a curve whose control point lies at x = 40000.5, off the page:
//     clamping it bends the curve the page shows.
//   - Page 4: numbers that place nothing, a grey level of 99999.5 set and
//     replaced at once and a marked-content property of 99999.5, which a
//     remediation clamps.
//   - Page 5: ten lines of text inside the limits, so the four characters
//     page 1 shows weigh under a tenth of the document's text: a clamp that
//     loses them passes an integrity probe that weighs the whole document.
//
// Every page draws in the conforming embedded TrueType of baretree_2b.pdf.
// There is no /Metadata and no /OutputIntents, so a remediation has its
// structural repairs to make besides.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fartext1b() ([]byte, error) {
	pdf := pdfgen.New()
	font := latinTrueTypeFont(pdf, "/LithoBareSans")
	lines := "BT /F1 12 Tf 14 TL 20 180 Td"
	for range 10 {
		lines += " (ABBAABBAABBA) '"
	}
	lines += " ET"
	for _, content := range []string{
		"1 0 0 1 0 -40000 cm BT /F1 24 Tf 1 0 0 1 20 40100.5 Tm (ABBA) Tj ET",
		"1 0 0 1 0 -40000.5 cm BT /F1 24 Tf 1 0 0 1 20 40100 Tm (BAAB) Tj ET",
		"BT /F1 24 Tf 20 160 Td (AB) Tj ET 4 w 20 20 m 40000.5 20 20 140 180 140 c S",
		"99999.5 g 0 g /Span << /X 99999.5 >> BDC BT /F1 24 Tf 20 100 Td (BA) Tj ET EMC 20 20 160 20 re f",
		lines,
	} {
		pdf.AddPage(pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		)))
	}
	return save(pdf, "1.4")
}
