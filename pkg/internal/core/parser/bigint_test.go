package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestParseIntegerBeyondInt64BecomesReal pins that a number literal too large
// for a 64-bit integer parses as a real rather than failing. Failing took the
// whole enclosing dictionary with it, and silently: a catalog holding such a
// value resolved to no keys at all.
func TestParseIntegerBeyondInt64BecomesReal(t *testing.T) {
	t.Parallel()
	src := []byte("<< /Big 9999999999999998901432077674033824231587840 /Type /Catalog >>")
	p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0)))
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject: %v", err)
	}
	d, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("got %T, want a dictionary", obj)
	}
	if len(d) != 2 {
		t.Fatalf("dictionary has %d keys, want 2: %v", len(d), d)
	}
	if _, ok := d["Big"].(ast.PdfReal); !ok {
		t.Errorf("/Big is %T, want ast.PdfReal", d["Big"])
	}
}
