package pdf_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestExtractTextReadsASimpleFontOneByteACode pins that a simple font's codes
// stay one byte each (ISO 32000-1, 9.6.1) under a /ToUnicode CMap that
// declares a two-byte codespace and maps byte pairs of the shown string. The
// litho fixture tounicodepair_2u.pdf shows "Hello world" in an embedded Type 1
// font whose CMap maps every shown code by its two-byte value, <006C> for
// "l", and also <6C6C> to "X" and <6F20> to "Y". Read two bytes a mapped pair,
// the line was "HeXYworld".
func TestExtractTextReadsASimpleFontOneByteACode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/tounicodepair_2u.pdf")
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
	if want := "Hello world"; text != want {
		t.Errorf("ExtractText = %q, want %q", text, want)
	}
}
