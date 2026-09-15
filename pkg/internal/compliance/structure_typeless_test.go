package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// typelessKidDoc builds a tagged document whose Document element holds a
// paragraph and one extra /K kid, kid, under PDF/UA-2 namespaces when ua2 is set.
func typelessKidDoc(t *testing.T, ua2 bool, kid string) []string {
	t.Helper()
	ns, nsList := "", ""
	if ua2 {
		ns, nsList = " /NS 8 0 R", " /Namespaces [8 0 R]"
	}
	body := "/P << /MCID 0 >> BDC 0 0 10 10 re f EMC"
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >>" + nsList + " >>",
		"<< /Type /StructElem /S /Document" + ns + " /P 4 0 R /K [6 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /P" + ns + " /P 5 0 R /Pg 3 0 R /K [0] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		fmt.Sprintf(kid, ns),
	}
}

// TestStructureElementWithoutType pins the non-standard structure type rules
// (ISO 14289-1, 7.1 t5; ISO 14289-2, 8.2.4 t1; ISO 19005-1, 6.8.3.4 t1; ISO
// 19005-2, 6.7.3.4 t1) on an element with no structure type. A dictionary whose
// /Type is StructElem, or absent with no /MCID or /Obj, is an element, and one
// whose /S is missing or null maps to no standard type. An untyped marked-content
// reference stays a content item (ISO 32000-1, Table 324 makes its /Type
// optional), where veraPDF reports it too. Every other case was measured
// against veraPDF.
func TestStructureElementWithoutType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	kids := []struct {
		name string
		kid  string
		want int
	}{
		{"a StructElem with no /S", "<< /Type /StructElem%s /P 5 0 R >>", 1},
		{"an untyped dictionary with no /S", "<< /P 5 0 R%s >>", 1},
		{"a StructElem whose /S is null", "<< /Type /StructElem /S null%s /P 5 0 R >>", 1},
		{"an untyped marked-content reference", "<< /MCID 1 /Pg 3 0 R%s >>", 0},
		{"a typed marked-content reference", "<< /Type /MCR /MCID 1 /Pg 3 0 R%s >>", 0},
		{"a paragraph", "<< /Type /StructElem /S /P%s /P 5 0 R >>", 0},
	}
	profiles := []struct {
		name string
		ua2  bool
		run  func(context.Context, []string) ([]error, error)
		rule Rule
	}{
		{"PDF/UA-1", false, func(ctx context.Context, objs []string) ([]error, error) {
			doc, _ := objsDocPDF(t, objs...)
			return ValidatePDFUA(ctx, doc, models.PDFUA1)
		}, Rule_14289_1_7_1_t05},
		{"PDF/UA-2", true, func(ctx context.Context, objs []string) ([]error, error) {
			doc, _ := objsDocPDF(t, objs...)
			return ValidatePDFUA(ctx, doc, models.PDFUA2)
		}, Rule_14289_2_8_2_4_t01},
		{"PDF/A-1a", false, func(ctx context.Context, objs []string) ([]error, error) {
			doc, _ := objsDocPDF(t, objs...)
			return ValidatePDFA(ctx, doc, models.PDFA1a)
		}, Rule_19005_1_6_8_3_4_t01},
		{"PDF/A-2a", false, func(ctx context.Context, objs []string) ([]error, error) {
			doc, _ := objsDocPDF(t, objs...)
			return ValidatePDFA(ctx, doc, models.PDFA2a)
		}, Rule_19005_2_6_7_3_4_t01},
	}
	for _, p := range profiles {
		for _, k := range kids {
			t.Run(p.name+": "+k.name, func(t *testing.T) {
				viols, err := p.run(ctx, typelessKidDoc(t, p.ua2, k.kid))
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != k.want {
					t.Errorf("%s t%d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, k.want)
				}
			})
		}
	}

	t.Run("PDF/A-2b judges no structure types", func(t *testing.T) {
		doc, _ := objsDocPDF(t, typelessKidDoc(t, false, "<< /Type /StructElem%s /P 5 0 R >>")...)
		viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_19005_2_6_7_3_4_t01); got != 0 {
			t.Errorf("6.7.3.4 t1 fired %d times, want 0", got)
		}
	})
}

// TestStructureUnderTypelessElement pins the PDF/UA-1 structure rules on the kids
// of an element with no structure type: veraPDF walks into it and judges its kids
// against a parent of no type, so a TR there is outside any table (ISO 14289-1,
// 7.2 t4), two H kids are two H in one node (7.4.4 t1), and a heading that skips
// a level still skips it (7.4.2 t1). Every case was measured against veraPDF.
func TestStructureUnderTypelessElement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		kids  string   // the typeless wrapper's /K entries
		inner []string // objects 9 and on
		rule  Rule
		want  int
	}{
		{"a TR", "9 0 R", []string{"<< /Type /StructElem /S /TR /P 8 0 R /K [10 0 R] >>", "<< /Type /StructElem /S /TD /P 9 0 R >>"}, Rule_14289_1_7_2_t04, 1},
		{"two H", "9 0 R 10 0 R", []string{"<< /Type /StructElem /S /H /P 8 0 R >>", "<< /Type /StructElem /S /H /P 8 0 R >>"}, Rule_14289_1_7_4_4_t01, 1},
		{"an H1 then an H3", "9 0 R 10 0 R", []string{"<< /Type /StructElem /S /H1 /P 8 0 R >>", "<< /Type /StructElem /S /H3 /P 8 0 R >>"}, Rule_14289_1_7_4_2_t01, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "/P << /MCID 0 >> BDC 0 0 10 10 re f EMC"
			objs := []string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R /StructParents 0 >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 8 0 R] >>",
				"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K [0] >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /StructElem /P 5 0 R /K [" + tt.kids + "] >>",
			}
			doc, _ := objsDocPDF(t, append(objs, tt.inner...)...)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
