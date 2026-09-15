package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNullLangIsAbsent pins ISO 14289-1, 7.2 on a /Lang that resolves to null,
// written as null or naming a null object: it is absent (ISO 32000-1, 7.3.7 and
// 7.3.10), not an invalid tag. On the catalog, text and outline items are then
// of undetermined language (t34, t2) and t29 stays silent; on a structure
// element under a catalog /Lang nothing fires. Every case was measured against
// veraPDF.
func TestNullLangIsAbsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const body = "/P << /MCID 0 >> BDC BT /F1 12 Tf 10 10 Td (Hi) Tj ET EMC"

	tests := []struct {
		name          string
		catLang       string
		elemLang      string
		t29, t34, t02 int
	}{
		{"a catalog /Lang written as null", " /Lang null", "", 0, 1, 1},
		{"a catalog /Lang naming a null object", " /Lang 11 0 R", "", 0, 1, 1},
		{"an element /Lang written as null", " /Lang (en)", " /Lang null", 0, 0, 0},
		{"an element /Lang naming a null object", " /Lang (en)", " /Lang 11 0 R", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >>"+tt.catLang+" /Outlines 9 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> /StructParents 0 >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Type /StructTreeRoot /K 7 0 R /ParentTree << /Nums [0 [8 0 R]] >> >>",
				"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0"+tt.elemLang+" >>",
				"<< /Type /Outlines /First 10 0 R /Last 10 0 R /Count 1 >>",
				"<< /Title (Chapter) /Parent 9 0 R /Dest [3 0 R /Fit] >>",
				"null",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			for _, c := range []struct {
				rule Rule
				want int
			}{{Rule_14289_1_7_2_t29, tt.t29}, {Rule_14289_1_7_2_t34, tt.t34}, {Rule_14289_1_7_2_t02, tt.t02}} {
				if got := countRuleErrs(t, viols, c.rule); got != c.want {
					t.Errorf("%s t%d fired %d times, want %d", c.rule.Clause, c.rule.TestNumber, got, c.want)
				}
			}
		})
	}
}

// TestNullDestOutputProfileIsAbsent pins the output intent profile rules
// (ISO 19005-1, 6.2.2 t1; ISO 19005-2, 6.2.3 t1) on a /DestOutputProfile that
// resolves to null: it names no profile, so no profile is judged, as with none
// at all. Every case was measured against veraPDF.
func TestNullDestOutputProfileIsAbsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, profile := range []struct {
		p    models.PDFAProfile
		rule Rule
	}{{models.PDFA1b, Rule_19005_1_6_2_2_t01}, {models.PDFA2b, Rule_19005_2_6_2_3_t01}} {
		for _, value := range []string{"null", "4 0 R"} {
			t.Run(fmt.Sprintf("%s /DestOutputProfile %s", profile.p, value), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R /OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1"+
						" /OutputConditionIdentifier (sRGB) /DestOutputProfile "+value+" >>] >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
					"null",
				)
				viols, err := ValidatePDFA(ctx, doc, profile.p)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, profile.rule); got != 0 {
					t.Errorf("%s t%d fired %d times, want 0", profile.rule.Clause, profile.rule.TestNumber, got)
				}
			})
		}
	}
}
