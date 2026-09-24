package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

func parseOne(t *testing.T, src string) ast.PdfObject {
	t.Helper()
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader([]byte(src)), int64(len(src)), 0))
	obj, err := NewParser(lex).ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject(%q): %v", src, err)
	}
	return obj
}

// TestParseArray_KeepsElementAfterMalformedToken pins that recovering from a
// bad array element costs only that element.
//
// ParseObject consumes a token before dispatching on it, so a bare keyword is
// already past when it reports one. Consuming again swallowed the element
// AFTER the bad token.
//
// # Corpus evidence
//
//   - pdfjs bug1953099.pdf: writes a lone "-" between two strings in a TJ
//     array, and the (e) that followed it vanished, so the word "extensively"
//     came out a letter short.
func TestParseArray_KeepsElementAfterMalformedToken(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"lone minus between strings", "[(v)-(e)(l)]", []string{"v", "e", "l"}},
		{"operator inside the array", "[(a) Tj (b)]", []string{"a", "b"}},
		{"two bad tokens in a row", "[(a)- -(b)]", []string{"a", "b"}},
		{"bad token last", "[(a)(b)-]", []string{"a", "b"}},
		{"bad token first", "[-(a)(b)]", []string{"a", "b"}},
	}

	for _, c := range cases {
		arr, ok := parseOne(t, c.src).(ast.PdfArray)
		if !ok {
			t.Errorf("%s: %q did not parse as an array", c.name, c.src)
			continue
		}
		var got []string
		for _, el := range arr {
			if s, isStr := el.(ast.PdfString); isStr {
				got = append(got, string(s))
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: %q gave strings %v, want %v", c.name, c.src, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %q gave strings %v, want %v", c.name, c.src, got, c.want)
				break
			}
		}
	}
}
