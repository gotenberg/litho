package parser

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestParseCollectsIntegerOverflows pins that an integer literal past the
// 64-bit range, read as a real, is listed in IntegerOverflows on every parse,
// and that a real literal or an integer inside the range is not: ISO 32000-1,
// 7.3.3 makes the literal an integer object, which a PDF/A validator judges
// against the integer limit.
func TestParseCollectsIntegerOverflows(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want []float64
	}{
		{"a positive literal", "[99999999999999999999]", []float64{1e20}},
		{"a negative literal", "[-99999999999999999999]", []float64{-1e20}},
		{"two literals in order", "[20000000000000000000 -99999999999999999999]", []float64{2e19, -1e20}},
		{"the largest 64-bit integer", "[9223372036854775807]", nil},
		{"a real literal of the same magnitude", "[99999999999999999999.0]", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := []byte(tt.src)
			p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0)))
			obj, err := p.ParseObject(context.Background())
			if err != nil {
				t.Fatalf("ParseObject: %v", err)
			}
			if _, ok := obj.(ast.PdfArray); !ok {
				t.Fatalf("got %T, want an array", obj)
			}
			if !slices.Equal(p.IntegerOverflows, tt.want) {
				t.Errorf("IntegerOverflows = %v, want %v", p.IntegerOverflows, tt.want)
			}
		})
	}
}
