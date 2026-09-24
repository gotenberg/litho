package filter

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeStream_NullFilter covers an explicit null /Filter, which is
// equivalent to no filter: the stream data is unencoded. qpdf emits this on
// cross-reference streams (xref-range.pdf); treating it as an invalid /Filter
// type made the whole document unreadable ("object not found in xref").
func TestDecodeStream_NullFilter(t *testing.T) {
	t.Parallel()
	raw := []byte("plaintext cross-reference stream data")
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfNull{}}, raw)

	result, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream with /Filter null must not error: %v", err)
	}
	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q (unchanged)", result, raw)
	}
}

// TestNewStreamReader_NullFilter pins that the lazy path reads an explicit
// null /Filter as [DecodeStream] does. It rejected it as an invalid /Filter
// type, so the same stream decoded eagerly and failed to open lazily.
func TestNewStreamReader_NullFilter(t *testing.T) {
	t.Parallel()
	raw := []byte("plaintext embedded file")
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfNull{}}, raw)

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatalf("NewStreamReader with /Filter null must not error: %v", err)
	}
	defer rc.Close()
	result, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q (unchanged)", result, raw)
	}
}
