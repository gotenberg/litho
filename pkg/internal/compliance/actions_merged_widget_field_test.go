package compliance

import (
	"context"
	"strings"
	"testing"
)

// TestMergedWidgetFieldActionJudgedOnce pins that an action reached by two
// paths is reported once.
//
// ISO 32000-1 §12.5.6.19 lets a terminal form field and its widget annotation be
// a single dictionary, so litho meets its /A and /AA twice: once walking the
// page's /Annots and once walking the AcroForm /Fields tree. veraPDF builds one
// PDAction per action object and reports it once.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: drew 117 findings from litho against veraPDF's 54,
//     split exactly 57 and 57 between the two paths over the same 42 merged
//     dictionaries.
func TestMergedWidgetFieldActionJudgedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 5 is the JavaScript action, referenced only from the merged
	// widget/field dictionary's /AA/F.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		// Merged: a Widget annotation that is also a terminal field (/FT, /T).
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /P 3 0 R"+
			" /FT /Tx /T (field1) /AA << /F 5 0 R >> >>",
		"<< /Type /Action /S /JavaScript /JS (app.alert\\(1\\);) >>",
	)

	got := checkActionGroup(ctx, &validationCtx{
		doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2,
	})

	forbidden := 0
	for _, v := range got {
		if strings.Contains(v.Context, "JavaScript") {
			forbidden++
		}
	}
	if forbidden != 1 {
		t.Errorf("one action object reached by two paths must report once, got %d (%v)",
			forbidden, got)
	}
}

// TestDistinctActionsStillEachReport pins the other side: deduping by action
// object must not collapse two different actions, nor silence a field whose
// action no annotation reaches.
func TestDistinctActionsStillEachReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Two separate action objects: one on the merged widget/field, one on a
	// field the page's /Annots never lists.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /P 3 0 R"+
			" /FT /Tx /T (field1) /AA << /F 5 0 R >> >>",
		"<< /Type /Action /S /JavaScript /JS (app.alert\\(1\\);) >>",
		// A field with no widget on any page: only the AcroForm walk reaches it.
		"<< /FT /Tx /T (field2) /AA << /F 7 0 R >> >>",
		"<< /Type /Action /S /JavaScript /JS (app.alert\\(2\\);) >>",
	)

	got := checkActionGroup(ctx, &validationCtx{
		doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2,
	})

	forbidden := 0
	for _, v := range got {
		if strings.Contains(v.Context, "JavaScript") {
			forbidden++
		}
	}
	if forbidden != 2 {
		t.Errorf("two distinct action objects must each report, got %d (%v)", forbidden, got)
	}
}
