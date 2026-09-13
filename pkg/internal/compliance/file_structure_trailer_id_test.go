package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestCheckTrailerID_ContentRequirementIsPart2Onwards pins how much the file
// identifier has to carry. ISO 19005-1 §6.1.3 t1 asks only that the trailer
// hold an /ID ("lastID != null"), so a part-1 file satisfies it whatever the
// entry contains. ISO 19005-2/3 §6.1.3 t1 and ISO 19005-4 §6.1.3 t1 add
// "lastID.length() > 0", and veraPDF measures that over the entry as a whole
// rather than per element.
//
// Measured against veraPDF on a five-cell matrix built from the veraPDF test
// suite's PDF_A-2b__veraPDF_test_suite_6-1-3-t01-fail-b.pdf by rewriting only
// its /ID: litho used to fail an /ID whose SECOND string was empty, which
// veraPDF passes at every part, and to fail every non-empty-but-contentless
// /ID at part 1, which veraPDF passes there.
func TestCheckTrailerID_ContentRequirementIsPart2Onwards(t *testing.T) {
	t.Parallel()

	hex := func(s string) ast.PdfObject { return ast.PdfString(s) }

	cases := []struct {
		name string
		id   ast.PdfObject
		// want per profile: PDF/A-1b, PDF/A-2b, PDF/A-4.
		want [3]bool
	}{
		{"both strings carry content", ast.PdfArray{hex("ab"), hex("cd")}, [3]bool{false, false, false}},
		{"only the second is empty", ast.PdfArray{hex("ab"), hex("")}, [3]bool{false, false, false}},
		{"only the first is empty", ast.PdfArray{hex(""), hex("cd")}, [3]bool{false, false, false}},
		{"both strings empty", ast.PdfArray{hex(""), hex("")}, [3]bool{false, true, true}},
		{"an empty array", ast.PdfArray{}, [3]bool{false, true, true}},
	}

	profiles := [3]string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, profile := range profiles {
				doc := &document.Document{Index: xref.Index{
					Trailer: ast.PdfDict{"ID": tc.id},
				}}
				got := len(checkTrailerID(doc, profile)) > 0
				if got != tc.want[i] {
					t.Errorf("%s with %s: violation = %v, want %v",
						profile, tc.name, got, tc.want[i])
				}
			}
		})
	}
}

// TestCheckTrailerID_AbsentIDFailsEveryPart pins the other side: an /ID the
// trailer does not carry at all leaves lastID null, which every part rejects.
func TestCheckTrailerID_AbsentIDFailsEveryPart(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4"} {
		doc := &document.Document{Index: xref.Index{Trailer: ast.PdfDict{}}}
		if len(checkTrailerID(doc, profile)) == 0 {
			t.Errorf("%s: a trailer with no /ID must fail §6.1.3 t1", profile)
		}
	}
}
