package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCodeWidthReadsTheCIDWidths pins that CodeWidth reads a Type 0 code's
// width at its CID, as text extraction advances by it: under Identity-H the
// two-byte code is the CID (ISO 32000-1, 9.7.5.2, Table 118) and /W gives it
// (9.7.4.3).
func TestCodeWidthReadsTheCIDWidths(t *testing.T) {
	t.Parallel()
	font := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type0"),
		"BaseFont": ast.PdfName("Test"),
		"Encoding": ast.PdfName("Identity-H"),
		"DescendantFonts": ast.PdfArray{ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("CIDFontType2"),
			"BaseFont": ast.PdfName("Test"),
			"DW":       ast.PdfInt(1000),
			"W":        ast.PdfArray{ast.PdfInt(258), ast.PdfArray{ast.PdfInt(250)}},
		}},
	}
	f, err := ResolveFontObject(context.Background(), &document.Document{Logger: document.NopLogger()}, font)
	if err != nil {
		t.Fatalf("ResolveFontObject: %v", err)
	}
	if got := f.CodeWidth([]byte{1, 2}); got != 250 {
		t.Errorf("CodeWidth(<0102>) = %v, want the /W width 250", got)
	}
	if got := f.CodeWidth([]byte{0, 9}); got != 1000 {
		t.Errorf("CodeWidth(<0009>) = %v, want /DW 1000", got)
	}
}

func TestStandard14StandIn(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"ABCDEF+ArialMT":     "Helvetica",
		"TimesNewRoman,Bold": "Times-Bold",
		"Courier":            "Courier",
	} {
		if got, ok := Standard14StandIn(name); !ok || got != want {
			t.Errorf("Standard14StandIn(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := Standard14StandIn("Melior"); ok {
		t.Error("Melior has a standard 14 stand-in")
	}
}
