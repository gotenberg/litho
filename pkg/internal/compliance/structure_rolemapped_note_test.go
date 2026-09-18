package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestRoleMappedNoteAndFormula pins that ISO 14289-1, 7.9 t1 and 7.7 t1 judge
// the structure type an element stands for through the /RoleMap, not the name
// it is written with. veraPDF builds its SENote and SEFormula objects from the
// mapped standard type: govdocs thread_400_400934.pdf writes /Footnote, mapped
// to /Note, without /ID, and veraPDF reports 7.9 t1. An unmapped /Footnote is no
// Note and passes. Every case was measured against veraPDF.
func TestRoleMappedNoteAndFormula(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		elem    string // object 7
		roleMap string
		rule    Rule
		want    bool
	}{
		{
			"a Footnote mapped to Note, without /ID", "<< /S /Footnote /K [8 0 R] /P 6 0 R >>",
			"<< /Footnote /Note >>", Rule_14289_1_7_9_t01, true,
		},
		{
			"a Footnote mapped to Note, with /ID", "<< /S /Footnote /K [8 0 R] /P 6 0 R /ID (n1) >>",
			"<< /Footnote /Note >>", Rule_14289_1_7_9_t01, false,
		},
		{
			"an unmapped Footnote without /ID", "<< /S /Footnote /K [8 0 R] /P 6 0 R >>",
			"<< >>", Rule_14289_1_7_9_t01, false,
		},
		{
			"a Note without /ID", "<< /S /Note /K [8 0 R] /P 6 0 R >>",
			"<< >>", Rule_14289_1_7_9_t01, true,
		},
		{
			"a name mapped to Formula, without /Alt", "<< /S /MyFormula /K [8 0 R] /P 6 0 R >>",
			"<< /MyFormula /Formula >>", Rule_14289_1_7_7_t01, true,
		},
		{
			"a name mapped to Formula, with /Alt", "<< /S /MyFormula /K [8 0 R] /P 6 0 R /Alt (x squared) >>",
			"<< /MyFormula /Formula >>", Rule_14289_1_7_7_t01, false,
		},
		{
			"a Formula without /Alt", "<< /S /Formula /K [8 0 R] /P 6 0 R >>",
			"<< >>", Rule_14289_1_7_7_t01, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /StructParents 0"+
					" /Resources << /Font << /F1 9 0 R >> >> >>",
				streamObj("/P <</MCID 0>> BDC BT /F1 12 Tf 10 50 Td (Hi) Tj ET EMC"),
				"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 10 0 R /RoleMap "+tt.roleMap+" >>",
				"<< /S /Document /K [7 0 R] /P 5 0 R >>",
				tt.elem,
				"<< /S /Span /Pg 3 0 R /K [0] /P 7 0 R >>",
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Nums [0 [8 0 R]] >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			fired := false
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == tt.rule.Clause && v.Rule.TestNumber == tt.rule.TestNumber {
					fired = true
				}
			}
			if fired != tt.want {
				t.Errorf("%s t%d fired = %v, want %v", tt.rule.Clause, tt.rule.TestNumber, fired, tt.want)
			}
		})
	}
}
