package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestForbiddenKeysWrittenAsNull pins the PDF/A forbidden-key rules on a key
// written as the null object and on one naming a null object. A direct null is
// an absent entry (ISO 32000-1, 7.3.7), so no rule fires on it. A reference is
// read by what veraPDF's accessor does with it: the keys it tests with knownKey
// (/TR, /OPI, /PS, /Ref, /Alternates, the PDF/A-1 /SMask rules,
// /NeedsRendering, /EmbeddedFiles, /OCProperties) still count it, and the keys
// it resolves (HalftoneName, the halftone TransferFunction, /Requirements,
// /AlternatePresentations, /PresSteps, the transparency an /SMask signals)
// do not (7.3.10). Every case was measured against veraPDF.
func TestForbiddenKeysWrittenAsNull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const ht = "/HT << /Type /Halftone /HalftoneType 1 /Frequency 60 /Angle 0 /SpotFunction /Round %s >>"

	type where struct{ cat, page, gs, form, img string }
	tests := []struct {
		name    string
		profile models.PDFAProfile
		at      func(v string) where
		rule    Rule
		wantRef int // with the key naming a null object; a direct null always wants 0
	}{
		{"ExtGState /TR", models.PDFA1b, func(v string) where { return where{gs: " /TR " + v} }, Rule_19005_1_6_2_8_t01, 1},
		{"ExtGState /TR, PDF/A-2", models.PDFA2b, func(v string) where { return where{gs: " /TR " + v} }, Rule_19005_2_6_2_5_t01, 1},
		{"halftone HalftoneName", models.PDFA2b, func(v string) where { return where{gs: " " + fmt.Sprintf(ht, "/HalftoneName "+v)} }, Rule_19005_2_6_2_5_t05, 0},
		{"halftone TransferFunction", models.PDFA2b, func(v string) where { return where{gs: " " + fmt.Sprintf(ht, "/TransferFunction "+v)} }, Rule_19005_2_6_2_5_t06, 0},
		{"form /OPI", models.PDFA1b, func(v string) where { return where{form: " /OPI " + v} }, Rule_19005_1_6_2_4_t02, 1},
		{"form /OPI, PDF/A-2", models.PDFA2b, func(v string) where { return where{form: " /OPI " + v} }, Rule_19005_2_6_2_9_t01, 1},
		{"image /OPI, PDF/A-2", models.PDFA2b, func(v string) where { return where{img: " /OPI " + v} }, Rule_19005_2_6_2_8_t02, 1},
		{"form /PS", models.PDFA1b, func(v string) where { return where{form: " /PS " + v} }, Rule_19005_1_6_2_5_t01, 1},
		{"form /Ref", models.PDFA1b, func(v string) where { return where{form: " /Ref " + v} }, Rule_19005_1_6_2_6_t01, 1},
		{"form /Ref, PDF/A-2", models.PDFA2b, func(v string) where { return where{form: " /Ref " + v} }, Rule_19005_2_6_2_9_t02, 1},
		{"image /Alternates", models.PDFA1b, func(v string) where { return where{img: " /Alternates " + v} }, Rule_19005_1_6_2_4_t01, 1},
		{"image /Alternates, PDF/A-2", models.PDFA2b, func(v string) where { return where{img: " /Alternates " + v} }, Rule_19005_2_6_2_8_t01, 1},
		{"image /SMask", models.PDFA1b, func(v string) where { return where{img: " /SMask " + v} }, Rule_19005_1_6_4_t02, 1},
		{"image /SMask, PDF/A-2 transparency", models.PDFA2b, func(v string) where { return where{img: " /SMask " + v} }, Rule_19005_2_6_2_10_t02, 0},
		{"ExtGState /SMask", models.PDFA1b, func(v string) where { return where{gs: " /SMask " + v} }, Rule_19005_1_6_4_t01, 1},
		{"ExtGState /SMask, PDF/A-2 transparency", models.PDFA2b, func(v string) where { return where{gs: " /SMask " + v} }, Rule_19005_2_6_2_10_t02, 0},
		{"catalog /Requirements", models.PDFA2b, func(v string) where { return where{cat: " /Requirements " + v} }, Rule_19005_2_6_11_t01, 0},
		{"catalog /NeedsRendering", models.PDFA2b, func(v string) where { return where{cat: " /NeedsRendering " + v} }, Rule_19005_2_6_4_2_t02, 1},
		{"names /AlternatePresentations", models.PDFA2b, func(v string) where { return where{cat: " /Names << /AlternatePresentations " + v + " >>"} }, Rule_19005_2_6_10_t01, 0},
		{"names /EmbeddedFiles", models.PDFA1b, func(v string) where { return where{cat: " /Names << /EmbeddedFiles " + v + " >>"} }, Rule_19005_1_6_1_11_t02, 1},
		{"page /PresSteps", models.PDFA2b, func(v string) where { return where{page: " /PresSteps " + v} }, Rule_19005_2_6_10_t02, 0},
		{"catalog /OCProperties", models.PDFA1b, func(v string) where { return where{cat: " /OCProperties " + v} }, Rule_19005_1_6_1_13_t01, 1},
	}
	for _, tt := range tests {
		for _, form := range []struct {
			name  string
			value string
			want  int
		}{{"written as null", "null", 0}, {"naming a null object", "8 0 R", tt.wantRef}} {
			t.Run(tt.name+" "+form.name, func(t *testing.T) {
				t.Parallel()
				w := tt.at(form.value)
				body := "q /GS0 gs /Fm0 Do /Im0 Do Q"
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R"+w.cat+" >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R"+
						" /Resources << /ExtGState << /GS0 5 0 R >> /XObject << /Fm0 6 0 R /Im0 7 0 R >> >>"+w.page+" >>",
					"<< /Length 27 >>\nstream\n"+body+"\nendstream",
					"<< /Type /ExtGState"+w.gs+" >>",
					"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10]"+w.form+" /Length 0 >>\nstream\n\nendstream",
					"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"+w.img+" /Length 1 >>\nstream\nA\nendstream",
					"null",
				)
				viols, err := ValidatePDFA(ctx, doc, tt.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, tt.rule); got != form.want {
					t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, form.want)
				}
			})
		}
	}
}
