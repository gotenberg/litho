package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestNameTreeFileSpecJudgedByPosition pins that a file specification the
// /Names /EmbeddedFiles tree names is judged whatever its /Type says.
//
// ISO 32000-1, 7.11.4 defines that tree as mapping name strings to file
// specification dictionaries, so position identifies one where the
// cross-reference sweep's /Type test cannot. pdfjs issue9367.pdf writes three
// specs as /Type /F, and litho saw none of them: veraPDF reports all three
// under 6.1.11 t1 at part 1 and under 6.8 / 6.9 at parts 2 to 4.
//
// The two controls are what keep this honest. A spec both the sweep and the
// tree reach must still be stated once, and a wrong-/Type dictionary the tree
// does NOT name must stay invisible, because the sweep's /Type test is its only
// discriminator over every object in a file.
func TestNameTreeFileSpecJudgedByPosition(t *testing.T) {
	t.Parallel()

	// notdefCount returns how many ISO 19005-1 6.1.11 t1 violations a document
	// carrying the given objects reports. The rule fires once per file
	// specification holding /EF, so it counts the specs litho found.
	count := func(t *testing.T, objs ...string) int {
		t.Helper()
		data := cyclePDF(objs...)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		n := 0
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.1.11" && v.Rule.TestNumber == 1 {
				n++
			}
		}
		return n
	}

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"
		content = "<< /Length 0 >>\nstream\n\nendstream"
		tree    = "<< /Names [(a.txt) 6 0 R] >>"
		stream  = "<< /Length 5 >>\nstream\nlitho\nendstream"

		wrongType = "<< /Type /F /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>"
		rightType = "<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>"
	)

	t.Run("a wrong /Type in the tree is still judged", func(t *testing.T) {
		t.Parallel()
		if got := count(t, catalog, pages, page, content, tree, wrongType, stream); got != 1 {
			t.Errorf("6.1.11 t1 violations = %d, want 1 (the tree names the spec whatever its /Type)", got)
		}
	})

	t.Run("a spec both paths reach is stated once", func(t *testing.T) {
		t.Parallel()
		if got := count(t, catalog, pages, page, content, tree, rightType, stream); got != 1 {
			t.Errorf("6.1.11 t1 violations = %d, want 1 (the sweep and the tree must not both state it)", got)
		}
	})

	t.Run("a wrong /Type the tree does not name stays invisible", func(t *testing.T) {
		t.Parallel()
		// Object 8 carries /EF with a wrong /Type and nothing references it, so
		// only the sweep could reach it, and its /Type test must reject it.
		got := count(t, catalog, pages, page, content, tree, rightType, stream,
			"<< /Type /F /F (b.txt) /UF (b.txt) /EF << /F 7 0 R >> >>")
		if got != 1 {
			t.Errorf("6.1.11 t1 violations = %d, want 1 (the sweep must stay keyed on /Type)", got)
		}
	})
}
