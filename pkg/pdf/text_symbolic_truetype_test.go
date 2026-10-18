package pdf_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestExtractTextReadsACopiedMacSubtableAsItsGlyphs pins that a symbolic
// TrueType font with no /Encoding and no (3,0) subtable reads a (1,0) subtable
// that copies its (3,1) subtable through the glyphs its codes select, and keeps
// Mac OS Roman for a genuine one (ISO 32000-1, 9.6.6.4). The litho fixture
// ttmaccopy_2b.pdf shows <C4E4> through M1, whose (1,0) subtable is the copy,
// and <808A> through M2, a genuine Mac OS Roman table over the same glyphs.
// Both read "Ää". Read as Mac OS Roman, M1 read "ƒ‰".
func TestExtractTextReadsACopiedMacSubtableAsItsGlyphs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/ttmaccopy_2b.pdf")
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
	if got := strings.Fields(text); len(got) != 2 || got[0] != "Ää" || got[1] != "Ää" {
		t.Errorf("ExtractText = %q, want the lines Ää and Ää", text)
	}
}
