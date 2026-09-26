package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// spanLangOpts parameterizes [spanLangDoc]: the page content stream plus the
// /Lang entries each level of the fixture may carry. An empty string omits the
// entry.
type spanLangOpts struct {
	stream       string
	catalogLang  string
	paraLang     string
	documentLang string
}

// spanLangDoc builds a single-page tagged PDF for the ISO 14289-1 §7.2
// t30/t31/t32 checks. Marked-content identifier 0 resolves through the page
// /StructParents key and the ParentTree to the /P structure element, whose /P
// chain climbs to /Document and then the structure tree root, so a test can
// place /Lang at any level and watch it reach the marked content.
func spanLangDoc(t *testing.T, opts spanLangOpts) (*document.Document, ast.PdfDict) {
	t.Helper()

	entry := func(lang string) string {
		if lang == "" {
			return ""
		}
		return fmt.Sprintf(" /Lang (%s)", lang)
	}

	objs := []string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R%s >>", entry(opts.catalogLang)),
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /StructParents 0 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(opts.stream), opts.stream),
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 8 0 R >>",
		fmt.Sprintf("<< /Type /StructElem /S /Document /P 5 0 R /K [7 0 R]%s >>", entry(opts.documentLang)),
		fmt.Sprintf("<< /Type /StructElem /S /P /P 6 0 R /K [<< /Type /MCR /Pg 3 0 R /MCID 0 >>]%s >>", entry(opts.paraLang)),
		"<< /Nums [0 [7 0 R]] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return doc, cat.Dict
}

// spanStream wraps a /Span property list in a tagged /P sequence carrying
// marked-content identifier 0.
//
// # Corpus evidence
//
//   - verapdf 7.2-t30, 7.2-t31, 7.2-t32: every file uses this shape.
func spanStream(paraProps, spanProps string) string {
	return fmt.Sprintf("/P << /MCID 0%s >> BDC\n/Span << %s >> BDC\nBT (x) Tj ET\nEMC\nEMC\n", paraProps, spanProps)
}

// TestSpanMarkedContentLang covers ISO 14289-1 §7.2 t30, t31 and t32: the
// natural language of text in the /ActualText, /Alt and /E entries of a /Span
// marked-content sequence must be determinable. Each rule fires only when no
// /Lang is reachable from the sequence, an enclosing sequence, the structure
// element owning the enclosing sequence's marked content, that element's /P
// ancestors, or the catalog.
func TestSpanMarkedContentLang(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		opts spanLangOpts
		t30  int
		t31  int
		t32  int
	}{
		{
			name: "no language reachable fires every entry",
			opts: spanLangOpts{stream: spanStream("", "/ActualText (rep) /Alt (alt) /E (exp)")},
			t30:  1, t31: 1, t32: 1,
		},
		{
			name: "own property list /Lang determines the language",
			opts: spanLangOpts{stream: spanStream("", "/ActualText (rep) /Alt (alt) /E (exp) /Lang (en-US)")},
		},
		{
			name: "empty own /Lang is present, so the language is determined",
			opts: spanLangOpts{stream: spanStream("", "/ActualText (rep) /Alt (alt) /E (exp) /Lang ()")},
		},
		{
			name: "enclosing marked-content sequence supplies /Lang",
			opts: spanLangOpts{stream: spanStream(" /Lang (en-US)", "/ActualText (rep) /Alt (alt) /E (exp)")},
		},
		{
			name: "owning structure element supplies /Lang",
			opts: spanLangOpts{
				stream:   spanStream("", "/ActualText (rep) /Alt (alt) /E (exp)"),
				paraLang: "en-US",
			},
		},
		{
			name: "structure ancestor supplies /Lang through the /P chain",
			opts: spanLangOpts{
				stream:       spanStream("", "/ActualText (rep) /Alt (alt) /E (exp)"),
				documentLang: "en-US",
			},
		},
		{
			name: "catalog /Lang determines the language document-wide",
			opts: spanLangOpts{
				stream:      spanStream("", "/ActualText (rep) /Alt (alt) /E (exp)"),
				catalogLang: "en-US",
			},
		},
		{
			name: "a non-Span tag is not a subject",
			opts: spanLangOpts{stream: spanStream("", "/ActualText (rep) /Alt (alt) /E (exp)")},
		},
		{
			name: "only the entries present are reported",
			opts: spanLangOpts{stream: spanStream("", "/Alt (alt)")},
			t31:  1,
		},
	}

	// The non-Span case reuses the fixture with its inner tag rewritten, so the
	// only difference from the firing case is the tag itself.
	tests[7].opts.stream = "/P << /MCID 0 >> BDC\n/Quote << /ActualText (rep) /Alt (alt) /E (exp) >> BDC\nBT (x) Tj ET\nEMC\nEMC\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := spanLangDoc(t, tt.opts)
			got := checkSpanMarkedContentLang(ctx, doc, cat, "PDF/UA-1")

			for _, want := range []struct {
				rule Rule
				n    int
			}{
				{Rule_14289_1_7_2_t30, tt.t30},
				{Rule_14289_1_7_2_t31, tt.t31},
				{Rule_14289_1_7_2_t32, tt.t32},
			} {
				if n := countRule(got, want.rule); n != want.n {
					t.Errorf("%s t%d: got %d violations, want %d (all: %v)",
						want.rule.Clause, want.rule.TestNumber, n, want.n, got)
				}
			}
		})
	}
}

// TestSpanMarkedContentLangProfileScope pins the rules to PDF/UA-1. The ISO
// 32005 profile behind PDF/UA-2 declares no SEMarkedContent rule, so the same
// document must stay silent there.
func TestSpanMarkedContentLangProfileScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := spanLangDoc(t, spanLangOpts{stream: spanStream("", "/ActualText (rep)")})
	for _, profile := range []string{"PDF/UA-2", "PDF/A-2a", "PDF/A-4"} {
		if got := checkSpanMarkedContentLang(ctx, doc, cat, profile); len(got) != 0 {
			t.Errorf("profile %s: got %d violations, want none: %v", profile, len(got), got)
		}
	}
}
