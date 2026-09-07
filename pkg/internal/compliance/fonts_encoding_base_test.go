package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveSimpleFontEncoding_ImplicitBaseEncoding pins that an /Encoding
// dictionary without /BaseEncoding keeps the embedded program's built-in
// encoding underneath its /Differences.
//
// Per ISO 32000-1, Table 114, an absent /BaseEncoding makes /Differences
// describe differences from an implicit base encoding, which for an embedded
// font program is that program's own built-in encoding. Reading only
// /Differences drops every code the array does not name: govdocs
// thread_000_000364.pdf lists just codes 9, 10, 13, 127 and 128 there and
// leaves every printing code to the CFF, so the shown codes resolved to no
// glyph and the width check (ISO 19005-2 §6.2.11.5) stayed silent on a font
// veraPDF reports.
func TestResolveSimpleFontEncoding_ImplicitBaseEncoding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}

	// encoding is the /Encoding entry of the font dictionary under test.
	resolve := func(t *testing.T, encoding string) map[byte]string {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /TEEUUH+Lato-Bold /FirstChar 160 /LastChar 160"+
				" /Widths [243] /Encoding "+encoding+" /FontDescriptor 5 0 R >>",
			"<< /Type /FontDescriptor /FontName /TEEUUH+Lato-Bold /Flags 34 /FontFile3 6 0 R >>",
			fmt.Sprintf("<< /Length %d /Subtype /Type1C >>\nstream\n%s\nendstream", len(cff), string(cff)),
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve font dict: %v", err)
		}
		return resolveSimpleFontEncoding(ctx, doc, fontDict)
	}

	t.Run("differences alone keep the program encoding as base", func(t *testing.T) {
		t.Parallel()
		enc := resolve(t, "<< /Type /Encoding /Differences [128 /quoteright] >>")
		if got := enc[128]; got != "quoteright" {
			t.Errorf("/Differences code 128 = %q, want %q", got, "quoteright")
		}
		if got := enc[160]; got != "space" {
			t.Errorf("code 160 = %q, want the program's built-in %q", got, "space")
		}
		if got := enc[65]; got != "A" {
			t.Errorf("code 65 = %q, want the program's built-in %q", got, "A")
		}
	})

	t.Run("an explicit base encoding wins over the program", func(t *testing.T) {
		t.Parallel()
		enc := resolve(t, "<< /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [128 /quoteright] >>")
		if got := enc[128]; got != "quoteright" {
			t.Errorf("/Differences code 128 = %q, want %q", got, "quoteright")
		}
		// WinAnsi names code 213 "Otilde"; the CFF built-in leaves it unset, so
		// seeing it proves the predefined table, not the program, is the base.
		if got := enc[0o325]; got != "Otilde" {
			t.Errorf("code 213 = %q, want WinAnsi's %q", got, "Otilde")
		}
	})

	t.Run("no PDF encoding falls back to the program", func(t *testing.T) {
		t.Parallel()
		enc := resolve(t, "null")
		if got := enc[160]; got != "space" {
			t.Errorf("code 160 = %q, want the program's built-in %q", got, "space")
		}
	})
}
