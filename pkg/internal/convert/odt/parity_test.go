package odt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// gotenbergDocument returns the bytes of one of Gotenberg's office test
// documents (MIT): from the odt corpus's sources, else from the directory
// LITHO_GOTENBERG_OFFICE names; the test skips without either.
func gotenbergDocument(t *testing.T, name string) []byte {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "test", "integration", "corpus", "gotenberg", "odt", "sources")
	candidates, _ := filepath.Glob(filepath.Join(root, "*", name))
	candidates = append(candidates, filepath.Join(root, name))
	if dir := os.Getenv("LITHO_GOTENBERG_OFFICE"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, name))
	}
	for _, c := range candidates {
		if data, err := os.ReadFile(c); err == nil {
			return data
		}
	}
	t.Skipf("%s is not in the odt corpus and LITHO_GOTENBERG_OFFICE is unset", name)
	return nil
}

// TestParity_GotenbergDocuments converts Gotenberg's ODT documents and
// compares the page count and the text, line by line in reading order,
// with what LibreOffice prints through Gotenberg's LibreOffice route.
//
// # Corpus evidence
//
//   - odt: memo.odt, memo.fodt, form.odt and form.fodt print on one page
//     with these lines (gotenberg/gotenberg:9.0.0, read with litho's text
//     extraction, the header and footer included).
func TestParity_GotenbergDocuments(t *testing.T) {
	memo := []string{
		"Northwind internal", "Purpose", "1",
		"This memo records the Q4 decisions. It has bold text and italic text and a footnote.",
		"Decisions", "Budget", "Budget approved for Q1.", "Hiring", "Two engineers will be hired.",
		"R1C1R1C2R1C3", "R2C1R2C2R2C3", "R3C1R3C2R3C3", "MEMO-END-MARKER", "1FOOTNOTE-MARKER text.", "Page 1",
	}
	form := []string{"FORM-DOC-MARKER", "Full name:", "Agree:  I agree", "Country:", "Comments:"}
	for _, tc := range []struct {
		name  string
		pages int
		lines []string
	}{
		{"memo.odt", 1, memo}, {"memo.fodt", 1, memo}, {"form.odt", 1, form}, {"form.fodt", 1, form},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := convertDoc(t, gotenbergDocument(t, tc.name), office.Options{})
			if len(res.Pages) != tc.pages {
				t.Fatalf("pages = %d, want %d", len(res.Pages), tc.pages)
			}
			if got, want := docText(res), strings.Join(tc.lines, "\n"); got != want {
				t.Errorf("text =\n%s\nwant\n%s", got, want)
			}
		})
	}
}
