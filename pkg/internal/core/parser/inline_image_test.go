package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestParseInlineImageHeaderStopsAfterID pins that the header of an inline
// image leaves the parser just past ID, reading no image data, and that
// ResumeAt reads on from the offset a caller gives it, and refuses while a
// token is read ahead.
func TestParseInlineImageHeaderStopsAfterID(t *testing.T) {
	t.Parallel()

	data := []byte("/W 1 /H 1 ID xx EI Q")
	p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)))
	dict, err := p.ParseInlineImageHeader(context.Background())
	if err != nil {
		t.Fatalf("ParseInlineImageHeader: %v", err)
	}
	if len(dict) != 2 {
		t.Errorf("dictionary = %v, want /W and /H", dict)
	}
	if got, want := p.Position(), int64(bytes.Index(data, []byte("ID"))+2); got != want {
		t.Fatalf("position = %d, want %d, just past ID", got, want)
	}
	if err := p.ResumeAt(int64(bytes.Index(data, []byte("Q")))); err != nil {
		t.Fatalf("ResumeAt: %v", err)
	}
	if tok, _ := p.Peek(0); tok.Value != "Q" {
		t.Errorf("next token = %q, want Q", tok.Value)
	}
	if err := p.ResumeAt(0); err == nil {
		t.Error("ResumeAt with a token read ahead succeeded")
	}
}
