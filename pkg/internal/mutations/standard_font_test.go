package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pendingType1Fonts returns every Type 1 font dictionary alloc holds.
func pendingType1Fonts(alloc *writer.ObjAllocator) []ast.PdfDict {
	var fonts []ast.PdfDict
	for _, obj := range alloc.Pending() {
		if d, ok := obj.(ast.PdfDict); ok && d["Type"] == ast.PdfName("Font") && d["Subtype"] == ast.PdfName("Type1") {
			fonts = append(fonts, d)
		}
	}
	return fonts
}

// checkCompleteStandardFont fails unless font states /FirstChar, /LastChar,
// /Widths and a /FontDescriptor that alloc holds, which ISO 32000-2, 9.6.2.2
// requires of a standard font and ISO 32000-1, 9.6.2.2 expects from PDF 1.5
// on.
func checkCompleteStandardFont(t *testing.T, alloc *writer.ObjAllocator, font ast.PdfDict) {
	t.Helper()
	for _, key := range []ast.PdfName{"FirstChar", "LastChar", "Widths"} {
		if _, ok := font[key]; !ok {
			t.Errorf("%v lacks %v", font["BaseFont"], key)
		}
	}
	ref, ok := font["FontDescriptor"].(ast.PdfIndirectRef)
	if !ok {
		t.Errorf("%v: /FontDescriptor %v is no indirect reference", font["BaseFont"], font["FontDescriptor"])
		return
	}
	if fd, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict); !ok || fd["Type"] != ast.PdfName("FontDescriptor") {
		t.Errorf("%v: descriptor %d is %v", font["BaseFont"], ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
}

// TestStampTextWritesACompleteStandardFont pins that a text stamp in a
// standard font writes the font with its widths and descriptor, for a font of
// WinAnsiEncoding and for one of a built-in encoding.
func TestStampTextWritesACompleteStandardFont(t *testing.T) {
	t.Parallel()

	for _, name := range []ast.PdfName{"Helvetica", "Times-Italic", "ZapfDingbats"} {
		ctx, doc, pages, alloc := stampDoc(t)
		if err := StampText(ctx, alloc, doc, pages, TextStampOptions{Text: "Stamp", FontSize: 12, StandardFont: name}, nil); err != nil {
			t.Fatalf("%s: StampText: %v", name, err)
		}
		fonts := pendingType1Fonts(alloc)
		if len(fonts) != 1 || fonts[0]["BaseFont"] != name {
			t.Fatalf("%s: the stamp wrote %d Type 1 fonts: %v", name, len(fonts), fonts)
		}
		checkCompleteStandardFont(t, alloc, fonts[0])
	}
}
