package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// langBDCDoc builds a tagged one-page document whose content stream carries a
// /Lang inside a BDC property dictionary, written exactly as bdcLang spells it.
//
// Two entries are load bearing rather than decorative. A /StructTreeRoot is
// required because checkPDFALangValid returns as soon as it cannot resolve one,
// so the inline scan would never run. /Resources is required because the scan
// sits behind a resources lookup that falls through to the next page when
// neither the page nor its parent declares any.
func langBDCDoc(t *testing.T, bdcLang string) *document.Document {
	t.Helper()
	body := "/Span << " + bdcLang + " >> BDC BT ET EMC"
	return cycleDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		"<< /Type /StructTreeRoot >>",
	)
}

// inlineBDCLangViolations returns only the violations the inline-BDC scan
// raised, so a malformed fixture cannot be mistaken for a passing case: the
// catalog and structure-element paths report a different Context.
func inlineBDCLangViolations(t *testing.T, bdcLang string) []Violation {
	t.Helper()
	doc := langBDCDoc(t, bdcLang)
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	var out []Violation
	for _, v := range checkPDFALangValid(context.Background(), doc, cat.Dict, "", 2) {
		if strings.Contains(v.Context, "inline BDC") {
			out = append(out, v)
		}
	}
	return out
}

// TestCheckPDFALangValid_InlineBDC covers the raw content-stream scan that reads
// a /Lang out of a BDC property dictionary. The function had no test at all, and
// the branch selecting between the literal-string and hex-string forms carried a
// dead sub-expression that no linter reports.
//
// The last case is the one that discriminates. A dictionary value is not a
// string, and reading "<<" as the start of a hex string decodes the dict's own
// punctuation into bytes and reports a language tag the file never wrote.
// Reducing the branch condition to a bare `rest[0] == '<'` makes exactly that
// case fail, which is what shows this test can tell the shapes apart.
func TestCheckPDFALangValid_InlineBDC(t *testing.T) {
	t.Parallel()

	// langTagAlphaNum demands an alpha first subtag, so "1" is invalid under
	// both grammars while "en" is valid under both.
	cases := []struct {
		name string
		bdc  string
		want int
	}{
		{"hex string, invalid tag", "/Lang <31>", 1},
		{"hex string, valid tag", "/Lang <656E>", 0},
		{"literal string, invalid tag", "/Lang (1)", 1},
		{"literal string, valid tag", "/Lang (en)", 0},
		{"dictionary value is not a hex string", "/Lang << /Foo 1 >>", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := inlineBDCLangViolations(t, tc.bdc)
			if len(got) != tc.want {
				t.Fatalf("inline BDC violations = %d, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}
