package parser

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// newParserFromBytes is a helper to create a Parser from raw bytes.
func newParserFromBytes(data []byte) *Parser {
	r := bytes.NewReader(data)
	reader := ioat.NewReader(r, int64(len(data)), 0)
	lex := lexer.NewLexer(reader)
	return NewParser(lex)
}

// FuzzParser feeds arbitrary bytes into the parser and ensures it never panics.
// The goal is not successful parsing. It is crash-freedom.
func FuzzParser(f *testing.F) {
	// Seed corpus: valid PDF objects that exercise different parser paths.

	// Simple types
	f.Add([]byte("null"))
	f.Add([]byte("true"))
	f.Add([]byte("false"))
	f.Add([]byte("42"))
	f.Add([]byte("-17"))
	f.Add([]byte("3.14"))
	f.Add([]byte("+0.5"))
	f.Add([]byte("-.002"))

	// Names
	f.Add([]byte("/Name"))
	f.Add([]byte("/Type"))
	f.Add([]byte("/"))

	// Strings
	f.Add([]byte("(hello world)"))
	f.Add([]byte("(nested (parens) here)"))
	f.Add([]byte("(escape \\n \\r \\t \\( \\) \\\\)"))
	f.Add([]byte("(octal \\101\\102\\103)"))
	f.Add([]byte("()"))

	// Hex strings
	f.Add([]byte("<48656C6C6F>"))
	f.Add([]byte("<48656C6C6F0>"))
	f.Add([]byte("<>"))

	// Arrays
	f.Add([]byte("[1 2 3]"))
	f.Add([]byte("[/Name (string) 42 true null]"))
	f.Add([]byte("[[1 2] [3 4]]"))
	f.Add([]byte("[]"))

	// Dictionaries
	f.Add([]byte("<< /Type /Catalog /Pages 3 0 R >>"))
	f.Add([]byte("<< /Key << /Nested true >> >>"))
	f.Add([]byte("<< >>"))
	f.Add([]byte("<< /A [1 2 3] /B (hello) /C <FF00> >>"))

	// Indirect references
	f.Add([]byte("10 0 R"))
	f.Add([]byte("999 0 R"))

	// Indirect objects
	f.Add([]byte("1 0 obj\n<< /Type /Page >>\nendobj"))
	f.Add([]byte("5 0 obj\n[1 2 3]\nendobj"))
	f.Add([]byte("7 0 obj\n(Hello)\nendobj"))

	// Streams
	f.Add([]byte("1 0 obj\n<< /Length 5 >>\nstream\nHello\nendstream\nendobj"))
	f.Add([]byte("2 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj"))

	// Adversarial: deeply nested structures
	f.Add([]byte("[[[[[[[[[[]]]]]]]]]]"))
	f.Add([]byte("<< /A << /B << /C << /D null >> >> >> >>"))

	// Adversarial: truncated input
	f.Add([]byte("<<"))
	f.Add([]byte("<< /Type"))
	f.Add([]byte("[1 2"))
	f.Add([]byte("1 0 obj"))
	f.Add([]byte("1 0 obj\n<< >>"))
	f.Add([]byte("(unterminated"))

	// Adversarial: garbage
	f.Add([]byte{0x00, 0x00, 0x00})
	f.Add([]byte{0xFF, 0xFE, 0xFD})
	f.Add([]byte("xyzzy"))
	f.Add([]byte(">>"))
	f.Add([]byte("]"))
	f.Add([]byte("endobj"))
	f.Add([]byte("stream\nendstream"))

	// Adversarial: huge number strings
	f.Add([]byte("99999999999999999999999999999999"))
	f.Add([]byte("1.79769313486231570e+308"))

	// Adversarial: mixed valid/invalid
	f.Add([]byte("<< /Type /Page >> endobj extra garbage"))
	f.Add([]byte("1 0 R extra tokens after ref"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Guard against inputs that are too large. The fuzzer can generate
		// multi-megabyte inputs that slow down the run without adding coverage.
		if len(data) > 64*1024 {
			return
		}

		p := newParserFromBytes(data)

		// Try to parse repeatedly until EOF or error. The parser must never panic.
		for range 1000 {
			obj, err := p.ParseObject()
			if err != nil {
				break
			}
			// Exercise String() on the result. Some panics hide in formatters.
			if obj != nil {
				_ = obj.String()
			}
		}
	})
}
