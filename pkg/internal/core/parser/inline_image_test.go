package parser

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

func TestSkipInlineImage_Basic(t *testing.T) {
	// After BI has been consumed, the remaining stream is:
	// /W 10 /H 5 /BPC 8 /CS /RGB ID <binary> EI Q
	data := []byte("/W 10 /H 5 /BPC 8 /CS /RGB ID \x00\x01\x02\xFF \nEI Q")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	// The next token should be the Q operator.
	tok, err := p.Peek(0)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_EIAtEOF(t *testing.T) {
	// EI is the last thing in the stream (followed only by EOF).
	data := []byte("/W 1 /H 1 /BPC 8 /CS /G ID \xFF EI")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	// Should be at EOF.
	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenEOF {
		t.Errorf("expected EOF, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_FalseEIInData(t *testing.T) {
	// Binary data contains "EI" without preceding whitespace — not the real terminator.
	data := []byte("/W 2 /H 1 /BPC 8 /CS /G ID \xAAEI\xBB \nEI\nQ")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_EmptyDict(t *testing.T) {
	// Minimal inline image with no dictionary entries.
	data := []byte("ID \x00 \nEI Q")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_EIFollowedByDelimiter(t *testing.T) {
	// EI followed by a delimiter character (/) instead of whitespace.
	data := []byte("/W 1 /H 1 /BPC 8 /CS /G ID \xFF \nEI/F1")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	// Next token should be the /F1 name.
	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenName || tok.Value != "/F1" {
		t.Errorf("expected /F1 name, got %v %q", tok.Type, tok.Value)
	}
}
