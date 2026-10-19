package pdf_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestExtractTextKeepsAnUnmappedLigatureInsideItsWord pins how a simple font
// reads codes no glyph name maps. The litho fixture texlig_2b.pdf shows, in an
// embedded Type 1 font with no /ToUnicode, "veri" 0x0C "cation" 0x20 "text"
// and "code" 0x20 "space", where /Differences names 0x0C T0C, a TeX-style "fi"
// ligature, and 0x20 T20, a glyph the program does not hold. T20 replaces the
// base encoding's space at 0x20 (ISO 32000-1, 9.6.6.1), so code 32 reads as a
// word space by 9.3.3 alone; 0x0C reads as no character, so the word
// stays whole. Reading every unmapped whitespace code as a space, as PDF.js
// does, gave "veri cation text". The litho corpus text cell scores the reading
// too, through an override of PDF.js's golden, which reads it that way.
func TestExtractTextKeepsAnUnmappedLigatureInsideItsWord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/texlig_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if want := "verication text\ncode space"; text != want {
		t.Errorf("ExtractText = %q, want %q", text, want)
	}
}
