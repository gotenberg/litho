package parser

import (
	"bytes"
	"context"
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

	if err := p.SkipInlineImage(context.Background()); err != nil {
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

	if err := p.SkipInlineImage(context.Background()); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	// Should be at EOF.
	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenEOF {
		t.Errorf("expected EOF, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_FalseEIInData(t *testing.T) {
	// Binary data contains "EI" without preceding whitespace, not the real terminator.
	data := []byte("/W 2 /H 1 /BPC 8 /CS /G ID \xAAEI\xBB \nEI\nQ")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(context.Background()); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_FalseEIFollowedByNUL(t *testing.T) {
	// FlateDecoded inline image data whose bytes contain "\rEI\x00": a
	// false terminator, because a genuine EI is followed by SPACE/LF/CR,
	// never NUL. The real terminator "\nEI " comes later. Regression for
	// spurious ISO 19005 §6.2.2 undefined-operator violations, where litho
	// stopped at the false EI and lexed the trailing image bytes
	// (\x82\xbb\x0f\xf8) as a content operator.
	data := []byte("/W 20 /H 1 /BPC 8 /F /Fl ID \x78\x9c\x63\x01\rEI\x00\x82\xbb\x0f\xf8\nEI Q")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(context.Background()); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword after real EI, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_EmptyDict(t *testing.T) {
	// Minimal inline image with no dictionary entries.
	data := []byte("ID \x00 \nEI Q")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(context.Background()); err != nil {
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

	if err := p.SkipInlineImage(context.Background()); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	// Next token should be the /F1 name.
	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenName || tok.Value != "/F1" {
		t.Errorf("expected /F1 name, got %v %q", tok.Type, tok.Value)
	}
}

func TestSkipInlineImage_ASCII85FalseEIWithDelimiter(t *testing.T) {
	// ASCII85-encoded image body whose data happens to contain the
	// bytes "<ws>EI<delim>" well before the real terminator. Without
	// filter-aware handling, the scanner would match that false EI
	// and leave the parser reading ASCII85 bytes as operators. With
	// /F /A85 declared, the scanner should require whitespace after
	// EI and skip past the false match to the real EI.
	data := []byte("/W 4 /H 1 /BPC 8 /CS /G /F /A85 ID Gb\"0;!=]#/!5bE/ABCD\nEI/X\r\n~>\nEI Q")
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := NewParser(lex)

	if err := p.SkipInlineImage(context.Background()); err != nil {
		t.Fatalf("SkipInlineImage failed: %v", err)
	}

	tok, _ := p.Peek(0)
	if tok.Type != lexer.TokenKeyword || tok.Value != "Q" {
		t.Errorf("expected Q keyword, got %v %q", tok.Type, tok.Value)
	}
}
