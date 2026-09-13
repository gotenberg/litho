package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFigureAndFormByStandardType pins the PDF/UA Figure and Form requirements
// on the structure type an element stands for: veraPDF builds its SEFigure and
// SEForm from the standard type, where the role map (or /RoleMapNS, for a
// namespaced type) applies first. So a name mapped to Figure or Form is judged,
// and a standard Figure or Form the role map remaps is not. It also pins that
// ISO 14289-2, 8.2.5.28.2 accepts /Alt or /ActualText alone: a Caption child
// does not stand in for them. Every case was measured against veraPDF.
func TestFigureAndFormByStandardType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		ua2NS   = " /Namespaces [7 0 R 8 0 R]"
		twoKids = " /K [<< /Type /OBJR /Obj 9 0 R >> << /Type /OBJR /Obj 10 0 R >>]"
	)
	tests := []struct {
		name string
		ua2  bool
		root string // StructTreeRoot entries beyond /K and /ParentTree
		elem string // object 6
		rule Rule
		want int
	}{
		{"UA-1: a Figure without /Alt", false, "", "<< /S /Figure /P 5 0 R >>", Rule_14289_1_7_3_t01, 1},
		{
			"UA-1: a name mapped to Figure", false, " /RoleMap << /MyFig /Figure >>",
			"<< /S /MyFig /P 5 0 R >>", Rule_14289_1_7_3_t01, 1,
		},
		{
			"UA-1: a Figure the role map remaps to Span", false, " /RoleMap << /Figure /Span >>",
			"<< /S /Figure /P 5 0 R >>", Rule_14289_1_7_3_t01, 0,
		},
		{"UA-1: a Figure with /Alt", false, "", "<< /S /Figure /P 5 0 R /Alt (a chart) >>", Rule_14289_1_7_3_t01, 0},

		{
			"UA-2: a PDF 2.0 Figure without /Alt", true, ua2NS, "<< /S /Figure /NS 7 0 R /P 5 0 R >>",
			Rule_14289_2_8_2_5_28_2_t01, 1,
		},
		{
			"UA-2: a Figure whose only alternative is a Caption", true, ua2NS,
			"<< /S /Figure /NS 7 0 R /P 5 0 R /K [11 0 R] >>", Rule_14289_2_8_2_5_28_2_t01, 1,
		},
		{
			"UA-2: a Figure with a Caption and /ActualText", true, ua2NS,
			"<< /S /Figure /NS 7 0 R /P 5 0 R /K [11 0 R] /ActualText (x) >>", Rule_14289_2_8_2_5_28_2_t01, 0,
		},
		{
			"UA-2: a name mapped to Figure through /RoleMapNS", true, ua2NS, "<< /S /MyFig /NS 8 0 R /P 5 0 R >>",
			Rule_14289_2_8_2_5_28_2_t01, 1,
		},
		{"UA-2: a Figure in no namespace", true, ua2NS, "<< /S /Figure /P 5 0 R >>", Rule_14289_2_8_2_5_28_2_t01, 1},
		{
			"UA-2: a PDF 2.0 Figure with /Alt", true, ua2NS, "<< /S /Figure /NS 7 0 R /P 5 0 R /Alt (a chart) >>",
			Rule_14289_2_8_2_5_28_2_t01, 0,
		},

		{
			"UA-1: a Form holding two widgets", false, "", "<< /S /Form /P 5 0 R /Pg 3 0 R" + twoKids + " >>",
			Rule_14289_1_7_18_4_t02, 1,
		},
		{
			"UA-1: a name mapped to Form holding two widgets", false, " /RoleMap << /MyForm /Form >>",
			"<< /S /MyForm /P 5 0 R /Pg 3 0 R" + twoKids + " >>", Rule_14289_1_7_18_4_t02, 1,
		},
		{
			"UA-1: a Form the role map remaps to Div", false, " /RoleMap << /Form /Div >>",
			"<< /S /Form /P 5 0 R /Pg 3 0 R" + twoKids + " >>", Rule_14289_1_7_18_4_t02, 0,
		},
		{
			"UA-1: a Form holding one widget", false, "",
			"<< /S /Form /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 9 0 R >>] >>", Rule_14289_1_7_18_4_t02, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			docNS := ""
			if tt.ua2 {
				docNS = " /NS 7 0 R"
			}
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en)"+
					" /AcroForm << /Fields [9 0 R 10 0 R] >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [9 0 R 10 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R 1 6 0 R] >>"+tt.root+" >>",
				"<< /S /Document"+docNS+" /P 4 0 R /K [6 0 R] >>",
				tt.elem,
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /Namespace /NS (http://example.com/ns) /RoleMapNS << /MyFig [/Figure 7 0 R] >> >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f0) /Rect [10 10 100 30] /F 4 /StructParent 0 /TU (field 0) >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f1) /Rect [10 40 100 60] /F 4 /StructParent 1 /TU (field 1) >>",
				"<< /S /Caption /NS 7 0 R /P 6 0 R >>",
			)
			profile := models.PDFUA1
			if tt.ua2 {
				profile = models.PDFUA2
			}
			viols, err := ValidatePDFUA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			got := 0
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule == tt.rule {
					got++
				}
			}
			if got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
