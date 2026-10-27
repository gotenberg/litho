package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// judgedAnnotsDoc builds a document carrying eight annotations that fail
// per-annotation rules at PDF/A and PDF/UA, listed the way shape says:
//
//   - "once": one page lists each annotation object once.
//   - "objects": two pages list the same annotation objects.
//   - "twice": one page lists each annotation object twice.
//   - "inline": two pages share one /Annots array holding the annotations
//     written inline.
//
// The Ink and Square annotations sit under Annot structure elements whose /Alt
// the Square's /Contents contradicts.
func judgedAnnotsDoc(t *testing.T, shape string) []string {
	t.Helper()
	annots := []string{
		"<< /Type /Annot /Subtype /Link /Rect [50 700 200 720] /F 4 /Border [0 0 0] >>",
		"<< /Type /Annot /Subtype /Text /Rect [50 650 70 670] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f1) /Rect [50 600 200 620] /F 4 /A << /S /JavaScript /JS (app.alert\\(1\\)) >> >>",
		"<< /Type /Annot /Subtype /Ink /Rect [50 550 100 580] /F 4 /InkList [[50 550 100 580]] /StructParent 0 >>",
		"<< /Type /Annot /Subtype /Stamp /Rect [50 500 100 530] /F 6 >>",
		"<< /Type /Annot /Subtype /Sound /Rect [50 450 70 470] /F 4 >>",
		"<< /Type /Annot /Subtype /TrapNet /Rect [50 400 70 420] /F 4 >>",
		"<< /Type /Annot /Subtype /Square /Rect [50 350 70 370] /F 5 /Contents (sq) /StructParent 1 >>",
	}
	refs := make([]string, len(annots))
	for i := range annots {
		refs[i] = fmt.Sprintf("%d 0 R", 10+i)
	}
	page := func(annotsEntry string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Tabs /S /Contents 9 0 R" + annotsEntry + " >>"
	}
	inkK, squareK := " /K << /Type /OBJR /Obj 13 0 R >>", " /K << /Type /OBJR /Obj 17 0 R >>"
	var page1, page2 string
	switch shape {
	case "once":
		page1, page2 = page(" /Annots ["+strings.Join(refs, " ")+"]"), page("")
	case "objects":
		page1 = page(" /Annots [" + strings.Join(refs, " ") + "]")
		page2 = page1
	case "twice":
		page1, page2 = page(" /Annots ["+strings.Join(refs, " ")+" "+strings.Join(refs, " ")+"]"), page("")
	case "inline":
		page1, page2 = page(" /Annots 18 0 R"), page(" /Annots 18 0 R")
		inkK, squareK = "", ""
	default:
		t.Fatalf("unknown shape %q", shape)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		page1,
		page2,
		"<< /Type /StructTreeRoot /K 6 0 R /ParentTree << /Nums [0 7 0 R 1 8 0 R] >> >>",
		"<< /Type /StructElem /S /Document /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /Annot /P 6 0 R /Alt (ink alt)" + inkK + " >>",
		"<< /Type /StructElem /S /Annot /P 6 0 R /Alt (different)" + squareK + " >>",
		exponentStream("", ""),
	}
	objs = append(objs, annots...)
	if shape == "inline" {
		objs = append(objs, "["+strings.Join(annots, " ")+"]")
	}
	return objs
}

// TestJudgedAnnotationsCountAsVeraPDF pins every per-annotation rule counted in
// the unit veraPDF judges an annotation in ([judgedAnnotations]): an
// annotation object two pages list, or one page lists twice, fails each rule
// as often as one page listing it once, and an annotation written inline in an
// /Annots array two pages share fails it once for each page. Every count was
// measured against veraPDF 1.30.2 on the four shapes of [judgedAnnotsDoc].
func TestJudgedAnnotationsCountAsVeraPDF(t *testing.T) {
	t.Parallel()
	type ruleCount struct {
		clause string
		test   int
		once   int
	}
	for _, tc := range []struct {
		name  string
		pdfa  models.PDFAProfile
		pdfua models.PDFUAProfile
		rules []ruleCount
	}{
		{name: "PDF/UA-1", pdfua: models.PDFUA1, rules: []ruleCount{
			{"7.18.1", 1, 3},
			{"7.18.1", 2, 4},
			{"7.18.1", 3, 1},
			{"7.18.2", 1, 1},
			{"7.18.4", 1, 1},
			{"7.18.5", 1, 1},
			{"7.18.5", 2, 1},
		}},
		{name: "PDF/UA-2", pdfua: models.PDFUA2, rules: []ruleCount{
			{"8.9.2.2", 1, 1},
			{"8.9.2.4.11", 1, 1},
			{"8.9.2.4.15", 1, 1},
			{"8.9.2.4.7", 1, 1},
			{"8.9.2.4.8", 1, 1},
			{"8.9.4.2", 1, 1},
		}},
		{name: "PDF/A-1b", pdfa: models.PDFA1b, rules: []ruleCount{
			{"6.5.2", 1, 1}, {"6.5.3", 2, 3}, {"6.6.1", 1, 1}, {"6.6.1", 3, 1}, {"6.9", 2, 1},
		}},
		{name: "PDF/A-2b", pdfa: models.PDFA2b, rules: []ruleCount{
			{"6.3.1", 1, 1}, {"6.3.2", 1, 1}, {"6.3.2", 2, 2}, {"6.3.3", 1, 7}, {"6.4.1", 1, 1}, {"6.5.1", 1, 1},
		}},
		{name: "PDF/A-4", pdfa: models.PDFA4, rules: []ruleCount{
			{"6.3.1", 1, 1}, {"6.3.2", 1, 1}, {"6.3.2", 2, 2}, {"6.3.3", 1, 7}, {"6.4.1", 1, 1},
		}},
	} {
		for _, shape := range []string{"once", "objects", "twice", "inline"} {
			t.Run(tc.name+"/"+shape, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, judgedAnnotsDoc(t, shape)...)
				var viols []error
				var err error
				if tc.pdfa != "" {
					viols, err = ValidatePDFA(context.Background(), doc, tc.pdfa)
				} else {
					viols, err = ValidatePDFUA(context.Background(), doc, tc.pdfua)
				}
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				for _, rc := range tc.rules {
					want := rc.once
					if shape == "inline" {
						want *= 2
					}
					got := 0
					for _, e := range viols {
						if v, ok := e.(Violation); ok && v.Rule.Clause == rc.clause && v.Rule.TestNumber == rc.test {
							got++
						}
					}
					if got != want {
						t.Errorf("%s t%d = %d, want %d", rc.clause, rc.test, got, want)
					}
				}
			})
		}
	}
}
