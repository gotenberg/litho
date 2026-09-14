package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicodeTrueTypeDifferencesNamesEveryShownCode pins that a TrueType whose
// /Differences names every code it shows needs no /ToUnicode entry. The names
// are stated outright rather than implied by a predefined table, so ISO 32000-1
// §9.10.2 derives a Unicode value from each without reading the program's cmap.
//
// veraPDF hangs ISO 19005-1 §6.3.8 t1 on its Glyph object and tests
// `toUnicode != null`, so it passes the shape: pdfjs issue8613.pdf shows two
// unembedded MSSansSerif fonts whose /Differences names the whole range, and
// govdocs thread_300_300859.pdf an embedded ZJLCSM+Symbol whose only shown code
// is named /bullet. litho reported both, and the same fonts again under
// ISO 19005-2 §6.2.11.7.2.
func TestToUnicodeTrueTypeDifferencesNamesEveryShownCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(profile, diffs string, usage *fontUsageInfo) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /TrueType /Name /F1 /BaseFont /LithoSans"+
				" /FirstChar 33 /LastChar 34 /Encoding 3 0 R /FontDescriptor 4 0 R >>",
			"<< /Type /Encoding /Differences "+diffs+" >>",
			"<< /Type /FontDescriptor /FontName /LithoSans /Flags 32 >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return checkToUnicode(ctx, doc, fontDict, "F1", 1, profile, usage)
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}

	for _, profile := range []string{"PDF/A-1a", "PDF/A-2u"} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()

			if got := build(profile, "[ 33 /bullet ]", used(33)); len(got) != 0 {
				t.Errorf("a shown code named by /Differences needs no /ToUnicode, got %v", got)
			}
			if got := build(profile, "[ 33 /bullet /quotedbl ]", used(33, 34)); len(got) != 0 {
				t.Errorf("every shown code named should stay exempt, got %v", got)
			}

			// The restriction this exemption must not loosen: a code the array
			// never names falls to the base encoding, which for a TrueType only
			// implies a name. ISO 32000-1 §9.6.6.4 resolves it through the
			// program's own cmap, which this check cannot read, and veraPDF
			// reports 16 govdocs files of exactly that shape.
			if got := build(profile, "[ 33 /bullet ]", used(33, 34)); len(got) == 0 {
				t.Error("a shown code /Differences does not name must still require /ToUnicode")
			}
			// A name with no Unicode value proves nothing either.
			if got := build(profile, "[ 33 /lithoglyphone ]", used(33)); len(got) == 0 {
				t.Error("an unresolvable /Differences name must still require /ToUnicode")
			}
			// Without the shown codes there is nothing to prove, so the
			// exemption must not fire on usage alone.
			if got := build(profile, "[ 33 /bullet ]", used()); len(got) == 0 {
				t.Error("no recorded codes must not earn the exemption by itself")
			}
		})
	}
}

// TestShownCodesNamedByResolvableDifferences pins the predicate directly, since
// it is the whole guard: it answers only for codes /Differences names.
func TestShownCodesNamedByResolvableDifferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(diffs string) (*document.Document, ast.PdfDict) {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /Font /Subtype /TrueType /Name /F1 /BaseFont /LithoSans /Encoding 3 0 R >>",
			"<< /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences "+diffs+" >>",
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		return doc, fontDict
	}
	used := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{visible: true, charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}

	cases := []struct {
		name  string
		diffs string
		usage *fontUsageInfo
		want  bool
	}{
		{"every shown code named and resolvable", "[ 65 /A /B ]", used(65, 66), true},
		{"a shown code the array never names", "[ 65 /A ]", used(65, 66), false},
		{"a named code with no Unicode value", "[ 65 /lithoglyphone ]", used(65), false},
		{"a .notdef override is another rule's business", "[ 65 /.notdef ]", used(65), true},
		{"no usage recorded", "[ 65 /A ]", used(), false},
		{"nil usage", "[ 65 /A ]", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, fontDict := build(tc.diffs)
			got := shownCodesNamedByResolvableDifferences(ctx, doc, fontDict, tc.usage)
			if got != tc.want {
				t.Errorf("shownCodesNamedByResolvableDifferences = %v, want %v", got, tc.want)
			}
		})
	}
}
