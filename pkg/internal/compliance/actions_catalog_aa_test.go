package compliance

import (
	"context"
	"testing"
)

// TestCheckCatalogAA_ForbiddenAAStillChecksActionTypes pins for the catalog
// /AA what [TestCheckPageActions_ForbiddenAAStillChecksActionTypes] pins for a
// page's. The dictionary is forbidden outright (ISO 19005-1 §6.6.2 t3, ISO
// 19005-2/3 §6.5.2 t1), but the action-type restrictions apply to every action
// in the file, so the actions it holds are still judged. litho reported the
// dictionary and returned, leaving the action unjudged.
//
// # Corpus evidence
//
//   - verapdf 6-5-2-t01-fail-b.pdf: its catalog /AA/WS holds a JavaScript action,
//     and veraPDF reports §6.6.1 t1 for the action and §6.6.2 t3 for the
//     dictionary.
func TestCheckCatalogAA_ForbiddenAAStillChecksActionTypes(t *testing.T) {
	t.Parallel()

	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AA << /WS 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Action /S /JavaScript /JS (app.alert\\(1\\);) >>",
	)

	for _, tc := range []struct {
		profile string
		part    int
		dict    Rule
		action  Rule
	}{
		{"PDF/A-1b", 1, Rule_19005_1_6_6_2_t03, Rule_19005_1_6_6_1_t01},
		{"PDF/A-2b", 2, Rule_19005_2_6_5_2_t01, Rule_19005_2_6_5_1_t01},
		{"PDF/A-3b", 3, Rule_19005_3_6_5_2_t01, Rule_19005_3_6_5_1_t01},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			got := checkCatalogAA(context.Background(), doc, cat, tc.profile, tc.part, make(map[int]bool))
			if !hasRule(got, tc.dict) {
				t.Errorf("%s: catalog /AA must report the forbidden dictionary %v (got %v)",
					tc.profile, tc.dict, got)
			}
			if !hasRule(got, tc.action) {
				t.Errorf("%s: catalog /AA must still judge its JavaScript action %v (got %v)",
					tc.profile, tc.action, got)
			}
		})
	}
}
