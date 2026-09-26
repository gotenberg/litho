package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestJavaScriptNameTreeJudgesItsActions pins how the action-type rules of ISO
// 19005-1, 6.6.1 t1, ISO 19005-2, 6.5.1 t1 and ISO 19005-4, 6.6.1 t1 read the
// catalog's /Names /JavaScript tree. ISO 32000-1, 7.7.4, Table 31 makes it a
// name tree of document-level JavaScript actions (12.6.4.16), which a reader
// runs when it opens the document, so each action the tree holds is judged
// like any other: one finding per action of a type the profile does not
// permit, and none for a tree that holds no action, is empty, or is written as
// null (ISO 32000-1, 7.3.7).
//
// veraPDF builds no PDAction from this tree, so it reports none of these; see
// [checkJavaScriptNameTree] for that adjudicated divergence.
func TestJavaScriptNameTreeJudgesItsActions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		js     = "<< /S /JavaScript /JS (app.alert(1)) >>"
		launch = "<< /S /Launch /F (a.exe) >>"
	)

	tests := []struct {
		name    string
		tree    string // the catalog /Names /JavaScript value
		obj4    string
		obj5    string
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{"one JavaScript action, PDF/A-2b", "4 0 R", "<< /Names [(a) 5 0 R] >>", js, models.PDFA2b, Rule_19005_2_6_5_1_t01, 1},
		{"one JavaScript action, PDF/A-1b", "4 0 R", "<< /Names [(a) 5 0 R] >>", js, models.PDFA1b, Rule_19005_1_6_6_1_t01, 1},
		{"one JavaScript action, PDF/A-3b", "4 0 R", "<< /Names [(a) 5 0 R] >>", js, models.PDFA3b, Rule_19005_3_6_5_1_t01, 1},
		{"one JavaScript action, PDF/A-4 permits it", "4 0 R", "<< /Names [(a) 5 0 R] >>", js, models.PDFA4, Rule_19005_4_6_6_1_t01, 0},
		{"two actions, one written inline", "4 0 R", "<< /Names [(a) 5 0 R (b) " + js + "] >>", js, models.PDFA2b, Rule_19005_2_6_5_1_t01, 2},
		{"an action in a kid node", "<< /Kids [4 0 R] >>", "<< /Limits [(a) (a)] /Names [(a) 5 0 R] >>", js, models.PDFA2b, Rule_19005_2_6_5_1_t01, 1},
		{"a Launch action, PDF/A-4 forbids it", "4 0 R", "<< /Names [(a) 5 0 R] >>", launch, models.PDFA4, Rule_19005_4_6_6_1_t01, 1},
		{"a permitted action type", "4 0 R", "<< /Names [(a) 5 0 R] >>", "<< /S /GoTo /D [0 /Fit] >>", models.PDFA2b, Rule_19005_2_6_5_1_t01, 0},
		{"an empty tree", "4 0 R", "<< >>", "null", models.PDFA2b, Rule_19005_2_6_5_1_t01, 0},
		{"an empty /Names array", "4 0 R", "<< /Names [] >>", "null", models.PDFA1b, Rule_19005_1_6_6_1_t01, 0},
		{"a tree written as null", "null", "null", "null", models.PDFA2b, Rule_19005_2_6_5_1_t01, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /Names << /JavaScript "+tt.tree+" >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				tt.obj4,
				tt.obj5,
			)
			viols, err := ValidatePDFA(ctx, doc, tt.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
