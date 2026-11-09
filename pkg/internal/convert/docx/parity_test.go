package docx

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// paritySources returns the directory Gotenberg's test documents sit under:
// LITHO_DOCX_SOURCES, else testdata/gotenberg.
func paritySources() string {
	if dir := os.Getenv("LITHO_DOCX_SOURCES"); dir != "" {
		return dir
	}
	return filepath.Join("testdata", "gotenberg")
}

// findSource returns the path of the file name under dir, "" when absent.
func findSource(dir, name string) string {
	found := ""
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error { //nolint:errcheck // a missing corpus skips the test
		if err == nil && !e.IsDir() && e.Name() == name {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// TestParity_GotenbergDocuments converts Gotenberg's Office test documents
// with the pinned fixture fonts and checks the page counts LibreOffice
// prints them on and their text in reading order.
//
// # Corpus evidence
//
//   - gotenberg/docx: page_1, pages_3, pages_12, Special_Chars_ß, i18n, memo-lo and
//     tracked print on 1, 3, 12, 1, 1, 1 and 1 pages and contract on 10
//     through Gotenberg 9.0.0's LibreOffice route.
func TestParity_GotenbergDocuments(t *testing.T) {
	t.Parallel()
	fonts := pinnedFonts(t)
	for _, tc := range []struct {
		file  string
		pages int
		text  []string
	}{
		{"page_1.docx", 1, []string{"Page 1"}},
		{"pages_3.docx", 3, []string{"Page 1", "Page 2", "Page 3"}},
		{"pages_12.docx", 12, []string{"Page 1", "Page 6", "Page 12"}},
		{"Special_Chars_ß.docx", 1, []string{"Page 1"}},
		{"i18n.docx", 1, []string{"Multilingual document", "ZH:", "你好", "JA:", "KO:", "RU:", "Привет", "MATH:"}},
		{"memo-lo.docx", 1, []string{"Northwind internal", "Page", "Purpose", "bold text", "Decisions", "Budget", "R1C1", "R3C3", "MEMO-END-MARKER", "FOOTNOTE-MARKER"}},
		{"tracked.docx", 1, []string{"Start of sentence.", "INSERTED-TEXT", "End of sentence."}},
		{"contract.docx", 10, []string{"Master Services Agreement", "1. Definitions", "Term", "Confidential Information", "8. Governing Law", "Schedule A lists"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			path := findSource(paritySources(), tc.file)
			if path == "" {
				t.Skipf("%s not in the docx corpus sources", tc.file)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Convert(context.Background(), bytes.NewReader(data), fonts, office.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != tc.pages {
				t.Errorf("%d pages, LibreOffice prints %d", len(doc.Pages), tc.pages)
			}
			all := docText(doc)
			at := 0
			for _, s := range tc.text {
				i := strings.Index(all[at:], s)
				if i < 0 {
					t.Errorf("%q missing after offset %d of %q", s, at, all)
					break
				}
				at += i + len(s)
			}
			if strings.Contains(all, "DELETED-TEXT") {
				t.Error("a deletion is printed")
			}
		})
	}
}
