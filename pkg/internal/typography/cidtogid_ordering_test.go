package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestApplyCIDToGIDIdentityFallback_Ordering pins that an explicit
// /CIDToGIDMap /Identity does not turn an Adobe CJK CID into a Unicode code
// point.
//
// An absent /CIDToGIDMap already means Identity (ISO 32000-1, 9.7.4.2), so
// writing the key explicitly must leave the text a document yields unchanged.
// It did not: a PDF/A fixer added the key, the fallback then read Adobe-Japan1
// CID 843 as U+034B, and "あい⽇本語" came back as mojibake.
func TestApplyCIDToGIDIdentityFallback_Ordering(t *testing.T) {
	ctx := context.Background()

	build := func(ordering string) *Font {
		doc := &document.Document{Logger: document.NopLogger()}
		cid := ast.PdfDict{
			"Type":        ast.PdfName("Font"),
			"Subtype":     ast.PdfName("CIDFontType2"),
			"CIDToGIDMap": ast.PdfName("Identity"),
			"CIDSystemInfo": ast.PdfDict{
				"Registry":   ast.PdfString("Adobe"),
				"Ordering":   ast.PdfString(ordering),
				"Supplement": ast.PdfInt(1),
			},
		}
		fontDict := ast.PdfDict{
			"Type":            ast.PdfName("Font"),
			"Subtype":         ast.PdfName("Type0"),
			"Encoding":        ast.PdfName("Identity-V"),
			"DescendantFonts": ast.PdfArray{cid},
		}
		font := &Font{}
		applyCIDToGIDIdentityFallback(ctx, doc, fontDict, font)
		return font
	}

	// Adobe CJK collections index their own character collection, so the CID
	// is not a code point and the fallback must stay off.
	for _, ordering := range []string{"Japan1", "GB1", "CNS1", "Korea1"} {
		if got := build(ordering).CIDIdentityFallback; got {
			t.Errorf("ordering %q: CIDIdentityFallback = true, want false (CID is not Unicode)", ordering)
		}
	}

	// Identity ordering carries no character collection, so treating the CID
	// as a code point remains the best available reading.
	if got := build("Identity").CIDIdentityFallback; !got {
		t.Error("ordering \"Identity\": CIDIdentityFallback = false, want true")
	}
}
